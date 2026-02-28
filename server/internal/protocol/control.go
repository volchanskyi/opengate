package protocol

// ControlMessageType identifies the variant of a control message.
// Must match Rust ControlMessage enum variant names for msgpack compat.
type ControlMessageType string

const (
	MsgAgentRegister ControlMessageType = "AgentRegister"
	MsgAgentHeartbeat ControlMessageType = "AgentHeartbeat"
	MsgSessionAccept  ControlMessageType = "SessionAccept"
	MsgSessionReject  ControlMessageType = "SessionReject"
	MsgSessionRequest ControlMessageType = "SessionRequest"
	MsgAgentUpdate    ControlMessageType = "AgentUpdate"
	MsgRelayReady     ControlMessageType = "RelayReady"
	MsgSwitchToWebRTC ControlMessageType = "SwitchToWebRTC"
	MsgSwitchAck      ControlMessageType = "SwitchAck"
	MsgIceCandidate   ControlMessageType = "IceCandidate"
)

// ControlMessage is the envelope for all control-plane messages.
// It uses msgpack encoding with named fields matching the Rust enum structure.
//
// The Rust side encodes ControlMessage as a msgpack map with a key indicating
// the variant. We mirror this by encoding as a map with the type name key.
type ControlMessage struct {
	Type ControlMessageType `msgpack:"type"`

	// AgentRegister
	Capabilities []AgentCapability `msgpack:"capabilities,omitempty"`
	Hostname     string            `msgpack:"hostname,omitempty"`
	OS           string            `msgpack:"os,omitempty"`

	// AgentHeartbeat
	Timestamp int64 `msgpack:"timestamp,omitempty"`

	// SessionAccept / SessionReject / SessionRequest
	Token    SessionToken `msgpack:"token,omitempty"`
	RelayURL string       `msgpack:"relay_url,omitempty"`
	Reason   string       `msgpack:"reason,omitempty"`

	// SessionRequest
	Permissions *Permissions `msgpack:"permissions,omitempty"`

	// AgentUpdate
	Version   string `msgpack:"version,omitempty"`
	URL       string `msgpack:"url,omitempty"`
	Signature string `msgpack:"signature,omitempty"`

	// SwitchToWebRTC
	SDPOffer string `msgpack:"sdp_offer,omitempty"`

	// IceCandidate
	Candidate string `msgpack:"candidate,omitempty"`
	Mid       string `msgpack:"mid,omitempty"`
}
