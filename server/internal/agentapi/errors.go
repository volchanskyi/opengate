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
	// ErrIncompleteControlMessage indicates the server tried to send a control
	// variant with a load-bearing field left empty. The encoder drops a
	// zero-valued field from the wire map and the agent's decoder requires it,
	// so the frame would break the agent's control stream; the send is refused
	// instead of putting an undecodable frame on the wire.
	ErrIncompleteControlMessage = errors.New("incomplete control message")
	// ErrConnectionClosed indicates the agent connection was closed.
	ErrConnectionClosed = errors.New("agent connection closed")
	// ErrLogsBusy indicates a raw-log pull is already in flight for the
	// connection. The broker serves one on-demand request at a time because
	// responses carry no correlation id.
	ErrLogsBusy = errors.New("device logs request already in flight")
	// ErrHistoryBusy indicates a deep-history pull is already in flight for the
	// connection. Like the log broker, one on-demand request runs at a time.
	ErrHistoryBusy = errors.New("device history request already in flight")
	// errEvidenceTooLargeInflated indicates an alert's compressed evidence
	// expands past anything the fixed composition could produce. A small blob
	// naming a huge one is the shape of a decompression bomb, not of evidence.
	errEvidenceTooLargeInflated = errors.New("alert evidence inflates past its bound")
	// errNoAlertStore indicates the server has no alert store wired, so an
	// admitted alert has nowhere to go.
	errNoAlertStore = errors.New("no alert store wired")
)
