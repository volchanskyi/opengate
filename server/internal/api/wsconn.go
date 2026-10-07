package api

import (
	"context"
	"time"

	"nhooyr.io/websocket"
)

// WSConn adapts a *websocket.Conn into a message-oriented relay.Conn that keeps message boundaries.
type WSConn struct {
	conn         *websocket.Conn
	label        string // "agent" or "browser" — used by handler-level logging
	writeTimeout time.Duration
}

// maxRelayMessageSize is the largest WebSocket message the relay accepts (4 MiB).
const maxRelayMessageSize = 4 << 20

// relayWriteTimeout bounds one forwarded frame, so a peer that stops draining frees its goroutine.
// TCP keep-alive does not catch a peer that is connected and not reading.
const relayWriteTimeout = 30 * time.Second

// NewWSConn wraps conn as a relay connection with the default write timeout.
func NewWSConn(conn *websocket.Conn, label string) *WSConn {
	return newWSConn(conn, label, relayWriteTimeout)
}

// newWSConn is NewWSConn with an explicit write timeout.
func newWSConn(conn *websocket.Conn, label string, writeTimeout time.Duration) *WSConn {
	conn.SetReadLimit(maxRelayMessageSize)
	return &WSConn{conn: conn, label: label, writeTimeout: writeTimeout}
}

// ReadMessage reads one complete binary message, with no deadline: a quiet session is legitimate.
// The handler's ping proves the peer is alive.
func (w *WSConn) ReadMessage() ([]byte, error) {
	_, data, err := w.conn.Read(context.Background())
	return data, err
}

// WriteMessage sends one complete binary message within the write timeout.
func (w *WSConn) WriteMessage(data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.writeTimeout)
	defer cancel()
	return w.conn.Write(ctx, websocket.MessageBinary, data)
}

// Close closes the connection with a normal closure status.
func (w *WSConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
