package agentapi

import "errors"

var (
	// ErrHandshakeFailed indicates the agent handshake could not complete.
	ErrHandshakeFailed = errors.New("handshake failed")
	// ErrUnexpectedMessage indicates an unexpected control message type.
	ErrUnexpectedMessage = errors.New("unexpected control message type")
	// ErrConnectionClosed indicates the agent connection was closed.
	ErrConnectionClosed = errors.New("agent connection closed")
)
