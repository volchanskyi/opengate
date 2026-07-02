package protocol

// ControlMessageType identifies the variant of a control message.
// Must match Rust ControlMessage enum variant names for msgpack compat.
type ControlMessageType string

const (
	MsgAgentRegister      ControlMessageType = "AgentRegister"
	MsgAgentHeartbeat     ControlMessageType = "AgentHeartbeat"
	MsgAgentHealthSummary ControlMessageType = "AgentHealthSummary"
	MsgAgentMetricWindow  ControlMessageType = "AgentMetricWindow"
	MsgProcessReport      ControlMessageType = "ProcessReport"
	MsgSessionAccept      ControlMessageType = "SessionAccept"
	MsgSessionReject      ControlMessageType = "SessionReject"
	MsgSessionRequest     ControlMessageType = "SessionRequest"
	MsgAgentUpdate        ControlMessageType = "AgentUpdate"
	MsgAgentUpdateAck     ControlMessageType = "AgentUpdateAck"
	MsgRelayReady         ControlMessageType = "RelayReady"
	MsgSwitchToWebRTC     ControlMessageType = "SwitchToWebRTC"
	MsgSwitchAck          ControlMessageType = "SwitchAck"
	MsgIceCandidate       ControlMessageType = "IceCandidate"

	// Input (browser → agent via relay)
	MsgMouseMove      ControlMessageType = "MouseMove"
	MsgMouseClick     ControlMessageType = "MouseClick"
	MsgKeyPress       ControlMessageType = "KeyPress"
	MsgTerminalResize ControlMessageType = "TerminalResize"

	// File operations
	MsgFileListRequest     ControlMessageType = "FileListRequest"
	MsgFileListResponse    ControlMessageType = "FileListResponse"
	MsgFileListError       ControlMessageType = "FileListError"
	MsgFileDownloadRequest ControlMessageType = "FileDownloadRequest"
	MsgFileUploadRequest   ControlMessageType = "FileUploadRequest"

	// Chat
	MsgChatMessage ControlMessageType = "ChatMessage"

	// Device lifecycle
	MsgAgentDeregistered     ControlMessageType = "AgentDeregistered"
	MsgRestartAgent          ControlMessageType = "RestartAgent"
	MsgRequestHardwareReport ControlMessageType = "RequestHardwareReport"
	MsgHardwareReport        ControlMessageType = "HardwareReport"
	MsgHardwareReportError   ControlMessageType = "HardwareReportError"
	MsgRequestDeviceLogs     ControlMessageType = "RequestDeviceLogs"
	MsgDeviceLogsResponse    ControlMessageType = "DeviceLogsResponse"
	MsgDeviceLogsError       ControlMessageType = "DeviceLogsError"
	MsgRequestHealthWindow   ControlMessageType = "RequestHealthWindow"
	MsgHealthWindowResponse  ControlMessageType = "HealthWindowResponse"
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

	// Edge Sentinel telemetry
	TS              int64                `msgpack:"ts,omitempty"`
	OrgID           string               `msgpack:"org_id,omitempty"`
	NodeAnomalyRate float64              `msgpack:"node_anomaly_rate,omitempty"`
	PerFamilyRates  []FamilyAnomalyRate  `msgpack:"per_family_rates,omitempty"`
	RecentBitmask   []byte               `msgpack:"recent_bitmask,omitempty"`
	SamplerVersion  string               `msgpack:"sampler_ver,omitempty"`
	ModelVersion    string               `msgpack:"model_ver,omitempty"`
	Dims            []MetricDim          `msgpack:"dims,omitempty"`
	TopN            []ProcessReportEntry `msgpack:"top_n,omitempty"`
	SinceTS         int64                `msgpack:"since_ts,omitempty"`
	Limit           uint32               `msgpack:"limit,omitempty"`
	Summaries       []HealthSummary      `msgpack:"summaries,omitempty"`

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

	// RequestDeviceLogs
	LogLevel  string `msgpack:"log_level,omitempty"`
	TimeFrom  string `msgpack:"time_from,omitempty"`
	TimeTo    string `msgpack:"time_to,omitempty"`
	Search    string `msgpack:"search,omitempty"`
	LogOffset uint32 `msgpack:"log_offset,omitempty"`
	LogLimit  uint32 `msgpack:"log_limit,omitempty"`

	// DeviceLogsResponse
	LogEntries []LogEntry `msgpack:"log_entries,omitempty"`
	TotalCount uint32     `msgpack:"total_count,omitempty"`
	HasMore    *bool      `msgpack:"has_more,omitempty"`
}

// LogEntry represents a single parsed log entry from the agent.
type LogEntry struct {
	Timestamp string `msgpack:"timestamp"`
	Level     string `msgpack:"level"`
	Target    string `msgpack:"target"`
	Message   string `msgpack:"message"`
}

// FamilyAnomalyRate is a per-family anomaly rate inside an Edge Sentinel summary.
type FamilyAnomalyRate struct {
	Family string  `msgpack:"family"`
	Rate   float64 `msgpack:"rate"`
}

// MetricDim is an averaged metric dimension in an Edge Sentinel metric window.
type MetricDim struct {
	Name string  `msgpack:"name"`
	Avg  float64 `msgpack:"avg"`
}

// ProcessReportEntry is a sanitized process sample row from Edge Sentinel.
type ProcessReportEntry struct {
	Rank        uint32  `msgpack:"rank"`
	Basename    string  `msgpack:"basename"`
	CmdlineHash *string `msgpack:"cmdline_hash,omitempty"`
	PID         uint32  `msgpack:"pid"`
	CPU         float64 `msgpack:"cpu"`
	Mem         float64 `msgpack:"mem"`
}

// HealthSummary is one bounded health summary point returned for read-back requests.
type HealthSummary struct {
	TS              int64               `msgpack:"ts"`
	OrgID           string              `msgpack:"org_id"`
	NodeAnomalyRate float64             `msgpack:"node_anomaly_rate"`
	PerFamilyRates  []FamilyAnomalyRate `msgpack:"per_family_rates"`
	RecentBitmask   []byte              `msgpack:"recent_bitmask"`
	SamplerVersion  string              `msgpack:"sampler_ver"`
	ModelVersion    string              `msgpack:"model_ver"`
}
