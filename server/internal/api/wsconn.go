package api

import (
	"context"

	"nhooyr.io/websocket"
)

// WSConn adapts a *websocket.Conn into a message-oriented relay.Conn.
// Each ReadMessage returns one complete WebSocket message, and each
// WriteMessage sends one complete WebSocket message, preserving boundaries.
type WSConn struct {
	conn  *websocket.Conn
	label string // "agent" or "browser" — used by handler-level logging
}

// maxRelayMessageSize is the maximum WebSocket message size the relay accepts.
// Matches the browser protocol codec's 16 MiB frame limit.
const maxRelayMessageSize = 16 << 20

// NewWSConn wraps a websocket.Conn into a relay-compatible connection.
func NewWSConn(conn *websocket.Conn, label string) *WSConn {
	conn.SetReadLimit(maxRelayMessageSize)
	return &WSConn{conn: conn, label: label}
}

// ReadMessage reads one complete binary message from the WebSocket.
func (w *WSConn) ReadMessage() ([]byte, error) {
	_, data, err := w.conn.Read(context.Background())
	return data, err
}

// WriteMessage sends one complete binary message over the WebSocket.
func (w *WSConn) WriteMessage(data []byte) error {
	return w.conn.Write(context.Background(), websocket.MessageBinary, data)
}

// Close closes the WebSocket connection with a normal closure status.
func (w *WSConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
