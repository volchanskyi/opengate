package api

import (
	"context"
	"log/slog"

	"nhooyr.io/websocket"
)

// WSConn adapts a *websocket.Conn into a message-oriented relay.Conn.
// Each ReadMessage returns one complete WebSocket message, and each
// WriteMessage sends one complete WebSocket message, preserving boundaries.
type WSConn struct {
	conn  *websocket.Conn
	label string // "agent" or "browser" for debug logging
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
	msgType, data, err := w.conn.Read(context.Background())
	if err != nil {
		slog.Error("[RELAY-DEBUG] WSConn.ReadMessage failed", "label", w.label, "error", err)
	} else {
		slog.Info("[RELAY-DEBUG] WSConn.ReadMessage OK", "label", w.label, "msgType", msgType, "bytes", len(data))
	}
	return data, err
}

// WriteMessage sends one complete binary message over the WebSocket.
func (w *WSConn) WriteMessage(data []byte) error {
	err := w.conn.Write(context.Background(), websocket.MessageBinary, data)
	if err != nil {
		slog.Error("[RELAY-DEBUG] WSConn.WriteMessage failed", "label", w.label, "bytes", len(data), "error", err)
	} else {
		slog.Info("[RELAY-DEBUG] WSConn.WriteMessage OK", "label", w.label, "bytes", len(data))
	}
	return err
}

// Close closes the WebSocket connection with a normal closure status.
func (w *WSConn) Close() error {
	slog.Info("[RELAY-DEBUG] WSConn.Close called", "label", w.label)
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
