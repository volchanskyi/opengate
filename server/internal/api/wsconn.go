package api

import (
	"context"
	"io"
	"sync"

	"nhooyr.io/websocket"
)

// WSConn adapts a *websocket.Conn into an io.ReadWriteCloser for use with
// the relay.Conn interface.
type WSConn struct {
	ctx    context.Context
	conn   *websocket.Conn
	reader io.Reader
	mu     sync.Mutex // protects reader
}

// NewWSConn wraps a websocket.Conn into a relay-compatible connection.
func NewWSConn(ctx context.Context, conn *websocket.Conn) *WSConn {
	return &WSConn{
		ctx:  ctx,
		conn: conn,
	}
}

// Read reads from the WebSocket connection. It reads complete binary messages,
// buffering partial reads within a single message.
func (w *WSConn) Read(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.reader == nil {
		_, reader, err := w.conn.Reader(w.ctx)
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
		_, reader, err := w.conn.Reader(w.ctx)
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
	err := w.conn.Write(w.ctx, websocket.MessageBinary, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the WebSocket connection with a normal closure status.
func (w *WSConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
