package api

import (
	"context"
	"io"
	"sync"

	"nhooyr.io/websocket"
)

// WSConn adapts a *websocket.Conn into an io.ReadWriteCloser for use with
// the relay.Conn interface. Context is not stored in the struct; each operation
// uses context.Background() because Close() terminates all pending I/O by
// closing the underlying WebSocket connection.
type WSConn struct {
	conn   *websocket.Conn
	reader io.Reader
	mu     sync.Mutex // protects reader
}

// NewWSConn wraps a websocket.Conn into a relay-compatible connection.
func NewWSConn(conn *websocket.Conn) *WSConn {
	return &WSConn{conn: conn}
}

// Read reads from the WebSocket connection. It reads complete binary messages,
// buffering partial reads within a single message.
func (w *WSConn) Read(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.reader == nil {
		_, reader, err := w.conn.Reader(context.Background())
		if err != nil {
			return 0, err
		}
		w.reader = reader
	}

	n, err := w.reader.Read(p)
	if err == io.EOF {
		// Current message consumed, reset for next message
		w.reader = nil
		if n > 0 {
			return n, nil
		}
		// Try to read the next message
		_, reader, err := w.conn.Reader(context.Background())
		if err != nil {
			return 0, err
		}
		w.reader = reader
		return w.reader.Read(p)
	}

	return n, err
}

// Write sends a binary message over the WebSocket connection.
func (w *WSConn) Write(p []byte) (int, error) {
	err := w.conn.Write(context.Background(), websocket.MessageBinary, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the WebSocket connection with a normal closure status.
// Closing the connection causes any pending Read or Write to return an error.
func (w *WSConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
