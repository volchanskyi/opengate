package api

import (
	"context"
	"time"

	"nhooyr.io/websocket"
)

// WSConn adapts a *websocket.Conn into a message-oriented relay.Conn.
// Each ReadMessage returns one complete WebSocket message, and each
// WriteMessage sends one complete WebSocket message, preserving boundaries.
type WSConn struct {
	conn         *websocket.Conn
	label        string // "agent" or "browser" — used by handler-level logging
	writeTimeout time.Duration
}

// maxRelayMessageSize is the maximum WebSocket message size the relay accepts (4 MiB).
// The agent protocol chunks data, so 4 MiB per message is sufficient.
const maxRelayMessageSize = 4 << 20

// relayWriteTimeout bounds one forwarded relay frame.
//
// It is a bound on the peer, not on the link. A peer that has stopped draining
// its socket lets the buffers fill, and the forwarding write then blocks with
// no error for as long as that peer stays connected — holding a goroutine and a
// socket per direction. TCP keep-alive does not reach this: it bounds a peer
// that vanished, not one that is present and not consuming. A frame a peer
// cannot accept in this budget is a peer that is not consuming.
const relayWriteTimeout = 30 * time.Second

// NewWSConn wraps a websocket.Conn into a relay-compatible connection.
func NewWSConn(conn *websocket.Conn, label string) *WSConn {
	return newWSConn(conn, label, relayWriteTimeout)
}

// newWSConn is NewWSConn with an explicit write budget, so a test can prove the
// deadline in milliseconds rather than in wall-clock seconds.
func newWSConn(conn *websocket.Conn, label string, writeTimeout time.Duration) *WSConn {
	conn.SetReadLimit(maxRelayMessageSize)
	return &WSConn{conn: conn, label: label, writeTimeout: writeTimeout}
}

// ReadMessage reads one complete binary message from the WebSocket.
//
// Deliberately undeadlined: a quiet relay session is legitimate — a technician
// watching a static screen sends nothing for minutes — so a read deadline would
// end sessions that are working. Proving the peer is still there is the
// handler's ping, not this read's job.
func (w *WSConn) ReadMessage() ([]byte, error) {
	_, data, err := w.conn.Read(context.Background())
	return data, err
}

// WriteMessage sends one complete binary message over the WebSocket, under
// relayWriteTimeout.
func (w *WSConn) WriteMessage(data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.writeTimeout)
	defer cancel()
	return w.conn.Write(ctx, websocket.MessageBinary, data)
}

// Close closes the WebSocket connection with a normal closure status.
func (w *WSConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
