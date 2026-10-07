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

	testData := []byte("hello websocket")
	require.NoError(t, conn.WriteMessage(testData))

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

	require.NoError(t, conn.Close())

	_, err := conn.ReadMessage()
	assert.Error(t, err)
}

func TestWSConn_MultipleMessages(t *testing.T) {
	t.Parallel()
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)
	defer conn.Close()

	for i := 0; i < 5; i++ {
		msg := []byte{byte(i)}
		require.NoError(t, conn.WriteMessage(msg))

		data, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, msg, data)
	}
}

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

func TestWSConn_WriteFailsAgainstAPeerThatNeverReads(t *testing.T) {
	t.Parallel()
	srv := wsSilentServer(t)
	defer srv.Close()

	stalled, elapsed := writeUntilError(t, srv.URL, 250*time.Millisecond)
	require.Error(t, stalled, "a write to a peer that never reads must not block indefinitely")
	assert.Lessf(t, elapsed, 10*time.Second,
		"the write must end on its own deadline rather than on the test's patience; took %s", elapsed)

	// The drain arm uses the shipped budget; a short one cuts off a draining peer on a loaded host.
	drain := wsDrainServer(t)
	defer drain.Close()
	drained, _ := writeUntilError(t, drain.URL, relayWriteTimeout)
	assert.NoError(t, drained, "a peer that drains must not be cut off by the write budget")
}

// The library closes the socket when the write context expires, so the error names the socket.
func writeUntilError(t *testing.T, serverURL string, budget time.Duration) (error, time.Duration) {
	t.Helper()
	rawConn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(serverURL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { rawConn.CloseNow() })
	conn := newWSConn(rawConn, "test", budget)

	frame := make([]byte, 1<<20)
	start := time.Now()
	for i := 0; i < 64; i++ {
		if err = conn.WriteMessage(frame); err != nil {
			break
		}
	}
	return err, time.Since(start)
}

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
