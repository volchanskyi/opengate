package protocol

// ControlMessageType identifies the variant of a control message.
// Must match Rust ControlMessage enum variant names for msgpack compat.
type ControlMessageType string

const (
	MsgAgentRegister  ControlMessageType = "AgentRegister"
	MsgAgentHeartbeat ControlMessageType = "AgentHeartbeat"
	MsgSessionAccept  ControlMessageType = "SessionAccept"
	MsgSessionReject  ControlMessageType = "SessionReject"
	MsgSessionRequest ControlMessageType = "SessionRequest"
	MsgAgentUpdate    ControlMessageType = "AgentUpdate"
	MsgAgentUpdateAck ControlMessageType = "AgentUpdateAck"
	MsgRelayReady     ControlMessageType = "RelayReady"
	MsgSwitchToWebRTC ControlMessageType = "SwitchToWebRTC"
	MsgSwitchAck      ControlMessageType = "SwitchAck"
	MsgIceCandidate   ControlMessageType = "IceCandidate"

	// Input (browser → agent via relay)
	MsgMouseMove      ControlMessageType = "MouseMove"
	MsgMouseClick     ControlMessageType = "MouseClick"
	MsgKeyPress       ControlMessageType = "KeyPress"
	MsgTerminalResize ControlMessageType = "TerminalResize"

	// File operations
	MsgFileListRequest    ControlMessageType = "FileListRequest"
	MsgFileListResponse   ControlMessageType = "FileListResponse"
	MsgFileListError      ControlMessageType = "FileListError"
	MsgFileDownloadRequest ControlMessageType = "FileDownloadRequest"
	MsgFileUploadRequest  ControlMessageType = "FileUploadRequest"

	// Chat
	MsgChatMessage ControlMessageType = "ChatMessage"

	// Device lifecycle
	MsgAgentDeregistered     ControlMessageType = "AgentDeregistered"
	MsgRestartAgent          ControlMessageType = "RestartAgent"
	MsgRequestHardwareReport ControlMessageType = "RequestHardwareReport"
	MsgHardwareReport        ControlMessageType = "HardwareReport"
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
	Arch         string            `msgpack:"arch,omitempty"`

	// AgentHeartbeat
	Timestamp int64 `msgpack:"timestamp,omitempty"`

	// SessionAccept / SessionReject / SessionRequest
	Token    SessionToken `msgpack:"token,omitempty"`
	RelayURL string       `msgpack:"relay_url,omitempty"`
	Reason   string       `msgpack:"reason,omitempty"`

	// SessionRequest
	Permissions *Permissions `msgpack:"permissions,omitempty"`

	// AgentRegister (version also used by AgentUpdate / AgentUpdateAck)
	Version string `msgpack:"version,omitempty"`

	// AgentUpdate
	URL       string `msgpack:"url,omitempty"`
	SHA256    string `msgpack:"sha256,omitempty"`
	Signature string `msgpack:"signature,omitempty"`

	// AgentUpdateAck
	Success  *bool  `msgpack:"success,omitempty"`
	AckError string `msgpack:"error,omitempty"`

	// SwitchToWebRTC
	SDPOffer string `msgpack:"sdp_offer,omitempty"`

	// IceCandidate
	Candidate string `msgpack:"candidate,omitempty"`
	Mid       string `msgpack:"mid,omitempty"`

	// MouseMove / MouseClick
	X      uint16 `msgpack:"x,omitempty"`
	Y      uint16 `msgpack:"y,omitempty"`
	Button string `msgpack:"button,omitempty"`

	// MouseClick / KeyPress
	Pressed *bool `msgpack:"pressed,omitempty"`

	// KeyPress
	Key string `msgpack:"key,omitempty"`

	// TerminalResize
	Cols uint16 `msgpack:"cols,omitempty"`
	Rows uint16 `msgpack:"rows,omitempty"`

	// FileListRequest / FileListResponse / FileDownloadRequest / FileUploadRequest
	Path    string      `msgpack:"path,omitempty"`
	Entries []FileEntry `msgpack:"entries,omitempty"`

	// FileUploadRequest
	TotalSize uint64 `msgpack:"total_size,omitempty"`

	// ChatMessage
	Text   string `msgpack:"text,omitempty"`
	Sender string `msgpack:"sender,omitempty"`

	// HardwareReport
	CPUModel          string             `msgpack:"cpu_model,omitempty"`
	CPUCores          uint32             `msgpack:"cpu_cores,omitempty"`
	RAMTotalMB        uint64             `msgpack:"ram_total_mb,omitempty"`
	DiskTotalMB       uint64             `msgpack:"disk_total_mb,omitempty"`
	DiskFreeMB        uint64             `msgpack:"disk_free_mb,omitempty"`
	NetworkInterfaces []NetworkInterface `msgpack:"network_interfaces,omitempty"`
}
