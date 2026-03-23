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
	srv := wsEchoServer(t)
	defer srv.Close()

	conn, _ := dialWSConn(t, srv.URL)

	// Close the adapter
	require.NoError(t, conn.Close())

	// Subsequent read should fail
	_, err := conn.ReadMessage()
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
			conn.WriteMessage([]byte{byte(i)})
		}(i)
	}

	// Concurrent reads (echo server sends back each message)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn.ReadMessage()
		}()
	}

	wg.Wait()
}
