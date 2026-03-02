package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

// wsEchoServer creates an httptest server that accepts a WebSocket and echoes messages.
// Returns the server and a channel that receives all messages written by the client.
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
	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	rawConn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	return NewWSConn(ctx, rawConn), rawConn
}

func TestWSConn_ReadWriteRoundtrip(t *testing.T) {
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)
	defer conn.Close()

	// Write data via adapter
	testData := []byte("hello websocket")
	n, err := conn.Write(testData)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)

	// Read echoed data via adapter
	buf := make([]byte, 256)
	n, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf[:n])
}

func TestWSConn_CloseClosesUnderlying(t *testing.T) {
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)

	// Close the adapter
	require.NoError(t, conn.Close())

	// Subsequent read should fail
	buf := make([]byte, 256)
	_, err := conn.Read(buf)
	assert.Error(t, err)
}

func TestWSConn_ConcurrentReadWrite(t *testing.T) {
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)
	defer conn.Close()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := []byte{byte(i)}
			conn.Write(data)
		}(i)
	}

	// Concurrent reads (echo server sends back each message)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 256)
			conn.Read(buf)
		}()
	}

	wg.Wait()
}
