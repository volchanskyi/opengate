package agentapi

import "errors"

var (
	// ErrHandshakeFailed indicates the agent handshake could not complete.
	ErrHandshakeFailed = errors.New("handshake failed")
	// ErrUnexpectedMessage indicates an unexpected control message type.
	ErrUnexpectedMessage = errors.New("unexpected control message type")
	// ErrCapabilityNotAdvertised indicates the server tried to send a control
	// variant the connected agent did not advertise support for.
	ErrCapabilityNotAdvertised = errors.New("agent capability not advertised")
	// ErrConnectionClosed indicates the agent connection was closed.
	ErrConnectionClosed = errors.New("agent connection closed")
)
