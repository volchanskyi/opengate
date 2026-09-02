package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

// wsEchoServer creates an httptest server that accepts a WebSocket and echoes messages.
func wsEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		conn.SetReadLimit(maxRelayMessageSize)

		for {
			msgType, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), msgType, data); err != nil {
				return
			}
		}
	}))
}

func dialWSConn(t *testing.T, serverURL string) (*WSConn, *websocket.Conn) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	rawConn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err)
	return NewWSConn(rawConn, "test"), rawConn
}

func TestWSConn_ReadWriteRoundtrip(t *testing.T) {
	t.Parallel()
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)
	defer conn.Close()

	// Write data via adapter
	testData := []byte("hello websocket")
	require.NoError(t, conn.WriteMessage(testData))

	// Read echoed data via adapter
	data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, testData, data)
}

func TestWSConn_LargeMessage(t *testing.T) {
	t.Parallel()
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)
	defer conn.Close()

	// 256 KB message — larger than the old 32KB io.CopyBuffer
	largeData := make([]byte, 256*1024)
	for i := range largeData {
		largeData[i] = byte(i % 251)
	}
	require.NoError(t, conn.WriteMessage(largeData))

	data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, largeData, data)
}

func TestWSConn_CloseClosesUnderlying(t *testing.T) {
	t.Parallel()
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)

	// Close the adapter
	require.NoError(t, conn.Close())

	// Subsequent read should fail
	_, err := conn.ReadMessage()
	assert.Error(t, err)
}

func TestWSConn_MultipleMessages(t *testing.T) {
	t.Parallel()
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)
	defer conn.Close()

	// nhooyr.io/websocket does not support concurrent reads or writes,
	// so verify sequential multi-message round-trips instead.
	for i := 0; i < 5; i++ {
		msg := []byte{byte(i)}
		require.NoError(t, conn.WriteMessage(msg))

		data, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, msg, data)
	}
}

// wsSilentServer accepts a WebSocket and then never reads from it, standing in
// for a peer that is present on the network and not consuming — which is the
// case TCP keep-alive cannot see, because the socket is alive.
func wsSilentServer(t *testing.T) *httptest.Server {
	t.Helper()
	accepted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		<-accepted
	}))
	t.Cleanup(func() { close(accepted) })
	return srv
}

// wsDrainServer accepts a WebSocket and reads everything sent to it, discarding
// it — a peer that is consuming as fast as it is given.
func wsDrainServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(maxRelayMessageSize)
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
}

// TestWSConn_WriteFailsAgainstAPeerThatNeverReads pins the deadline on a
// forwarded relay frame.
//
// Against a peer that accepted the connection and never drains it, the socket
// buffers fill and the write blocks. Without a deadline it blocks with no error
// for as long as the peer stays connected, holding a goroutine and a socket per
// direction. A frame the peer cannot accept inside the budget is a peer that is
// not consuming, not a slow link.
func TestWSConn_WriteFailsAgainstAPeerThatNeverReads(t *testing.T) {
	t.Parallel()
	srv := wsSilentServer(t)
	defer srv.Close()

	stalled, elapsed := writeUntilError(t, srv.URL)
	require.Error(t, stalled, "a write to a peer that never reads must not block indefinitely")
	assert.Lessf(t, elapsed, 10*time.Second,
		"the write must end on its own deadline rather than on the test's patience; took %s", elapsed)

	// The contrast is what makes the arm above specific: the identical loop
	// against a peer that drains carries every frame and returns no error. It
	// has to be a drain rather than the echo server — an echo whose replies
	// nobody reads fills its own buffers, stops reading, and stalls exactly
	// like the silent peer.
	drain := wsDrainServer(t)
	defer drain.Close()
	drained, _ := writeUntilError(t, drain.URL)
	assert.NoError(t, drained, "a peer that drains must not be cut off by the write budget")
}

// writeUntilError writes frames through a WSConn with a short write budget until
// one fails, and reports the failure and how long it took. It writes enough to
// fill the send and receive buffers on any host this runs on; the write that
// finds them full is the one under test.
//
// The error is not matched against context.DeadlineExceeded: when the write
// context expires the library tears the connection down, and the write that
// returns names the closed socket rather than the deadline that closed it. What
// is being asserted is that the write ends at all, which without a budget it
// does not.
func writeUntilError(t *testing.T, serverURL string) (error, time.Duration) {
	t.Helper()
	rawConn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(serverURL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { rawConn.CloseNow() })
	conn := newWSConn(rawConn, "test", 250*time.Millisecond)

	frame := make([]byte, 1<<20)
	start := time.Now()
	for i := 0; i < 64; i++ {
		if err = conn.WriteMessage(frame); err != nil {
			break
		}
	}
	return err, time.Since(start)
}

// TestWSConn_ReadIsNotDeadlined states the other half deliberately. A quiet
// relay session is legitimate — a technician watching a static screen sends
// nothing for minutes — so a read deadline would end sessions that are working.
// Liveness is the ping's job, not the read's.
func TestWSConn_ReadIsNotDeadlined(t *testing.T) {
	t.Parallel()
	srv := wsSilentServer(t)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	rawConn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err)
	conn := newWSConn(rawConn, "test", 250*time.Millisecond)

	read := make(chan error, 1)
	go func() {
		_, err := conn.ReadMessage()
		read <- err
	}()

	select {
	case err := <-read:
		t.Fatalf("a quiet session must not be ended by a read deadline; read returned %v", err)
	case <-time.After(time.Second):
	}
	rawConn.CloseNow()
	select {
	case <-read:
	case <-time.After(3 * time.Second):
		t.Fatal("closing the connection must end the read")
	}
}
