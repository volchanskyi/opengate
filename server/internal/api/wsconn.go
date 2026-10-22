package api

import (
	"context"
	"time"

	"github.com/coder/websocket"

	"github.com/volchanskyi/opengate/server/internal/relay"
)

// WSConn adapts a *websocket.Conn into a message-oriented relay.Conn that keeps message boundaries.
type WSConn struct {
	conn         *websocket.Conn
	label        string // "agent" or "browser" — used by handler-level logging
	writeTimeout time.Duration
}

// relayWriteTimeout bounds one forwarded frame, so a peer that stops draining frees its goroutine.
// TCP keep-alive does not catch a peer that is connected and not reading.
const relayWriteTimeout = 30 * time.Second

// NewWSConn wraps conn as a relay connection with the default write timeout.
func NewWSConn(conn *websocket.Conn, label string) *WSConn {
	return newWSConn(conn, label, relayWriteTimeout)
}

// newWSConn is NewWSConn with an explicit write timeout.
func newWSConn(conn *websocket.Conn, label string, writeTimeout time.Duration) *WSConn {
	conn.SetReadLimit(relay.MaxMessageSize)
	return &WSConn{conn: conn, label: label, writeTimeout: writeTimeout}
}

// ReadMessage reads one complete binary message into a pooled buffer, with no deadline: a quiet
// session is legitimate. The handler's ping proves the peer is alive.
func (w *WSConn) ReadMessage() (relay.Message, error) {
	_, r, err := w.conn.Reader(context.Background())
	if err != nil {
		return relay.Message{}, err
	}
	return relay.ReadMessageFrom(r)
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

// StatusSessionEnded is the close code of a session ended from outside it; the reason names why.
const StatusSessionEnded websocket.StatusCode = 4000

// End closes the connection with StatusSessionEnded and reason, which the browser shows.
func (w *WSConn) End(reason string) error {
	return w.conn.Close(StatusSessionEnded, reason)
}
