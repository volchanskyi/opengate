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

	// Edge-Sentinel WS-15 offline reconnect-backfill.
	MsgRequestBackfillSlot  ControlMessageType = "RequestBackfillSlot"
	MsgGrantBackfill        ControlMessageType = "GrantBackfill"
	MsgDeferBackfill        ControlMessageType = "DeferBackfill"
	MsgMetricBackfillBatch  ControlMessageType = "MetricBackfillBatch"
	MsgMetricBackfillAck    ControlMessageType = "MetricBackfillAck"
	MsgRequestLocalHistory  ControlMessageType = "RequestLocalHistory"
	MsgLocalHistoryResponse ControlMessageType = "LocalHistoryResponse"

	// Edge-Sentinel WS-16 auto-discovery inventory report.
	MsgDiscoveryReport ControlMessageType = "DiscoveryReport"

	// Edge-Sentinel WS-19 server → agent threshold-alert ruleset push.
	MsgPushAlertRules ControlMessageType = "PushAlertRules"

	// Maintenance mode: server → agent toggle and the agent's applied-state report.
	MsgSetMaintenanceMode ControlMessageType = "SetMaintenanceMode"
	MsgMaintenanceApplied ControlMessageType = "MaintenanceApplied"
)

// AlertComparator is the comparison direction of a WS-19 threshold-alert rule.
// It mirrors the Rust AlertComparator enum, which serializes as its variant
// name.
type AlertComparator string

const (
	// AlertComparatorGt breaches while the metric is strictly greater than the threshold.
	AlertComparatorGt AlertComparator = "Gt"
	// AlertComparatorLt breaches while the metric is strictly less than the threshold.
	AlertComparatorLt AlertComparator = "Lt"
	// AlertComparatorGte breaches while the metric is >= the threshold.
	AlertComparatorGte AlertComparator = "Gte"
	// AlertComparatorLte breaches while the metric is <= the threshold.
	AlertComparatorLte AlertComparator = "Lte"
)

// BackfillTier identifies which central VictoriaMetrics tier a reconnect
// backfill batch targets. It mirrors the Rust BackfillTier enum, which
// serializes as its variant name. Full-res 1 s raw is never a backfill tier —
// it is reachable only via an on-demand deep-history pull.
type BackfillTier string

const (
	// BackfillTierRaw10s carries 10 s windows rolled from local T0 → VM raw tier.
	BackfillTierRaw10s BackfillTier = "Raw10s"
	// BackfillTierRollup1m carries 1 min points from local T1 → VM 1 min rollup.
	BackfillTierRollup1m BackfillTier = "Rollup1m"
	// BackfillTierRollup1h carries 1 hr points from local T2 → VM 1 hr rollup.
	BackfillTierRollup1h BackfillTier = "Rollup1h"
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

	// Edge-Sentinel WS-19 threshold alerts. Breaches ride an AgentHealthSummary
	// (agent → server); AlertRules ride a PushAlertRules (server → agent).
	Breaches   []AlertBreach   `msgpack:"breaches,omitempty"`
	AlertRules []ThresholdRule `msgpack:"rules,omitempty"`

	// Edge-Sentinel WS-15 reconnect-backfill scheduler + tiered replay.
	PendingSamples  uint64           `msgpack:"pending_samples,omitempty"`
	OldestTS        int64            `msgpack:"oldest_ts,omitempty"`
	Rate            uint32           `msgpack:"rate,omitempty"`
	Deadline        int64            `msgpack:"deadline,omitempty"`
	RetryAfter      uint32           `msgpack:"retry_after,omitempty"`
	Tier            BackfillTier     `msgpack:"tier,omitempty"`
	BackfillSamples []BackfillSample `msgpack:"samples,omitempty"`
	Cursor          int64            `msgpack:"cursor,omitempty"`
	// On-demand deep-history pull (single dimension, bounded window).
	Dim           string         `msgpack:"dim,omitempty"`
	FromTS        int64          `msgpack:"from_ts,omitempty"`
	ToTS          int64          `msgpack:"to_ts,omitempty"`
	MaxPoints     uint32         `msgpack:"max_points,omitempty"`
	HistoryPoints []HistoryPoint `msgpack:"points,omitempty"`
	Truncated     *bool          `msgpack:"truncated,omitempty"`

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
	// Source selects the host log source ("self", "journald", "windows"); empty
	// means the agent's own files. Unit is a structured emitting-unit filter.
	Source string `msgpack:"source,omitempty"`
	Unit   string `msgpack:"unit,omitempty"`

	// DeviceLogsResponse
	LogEntries []LogEntry `msgpack:"log_entries,omitempty"`
	TotalCount uint32     `msgpack:"total_count,omitempty"`
	HasMore    *bool      `msgpack:"has_more,omitempty"`

	// DiscoveryReport (WS-16). TS/OrgID/Truncated are shared with the fields
	// above. Each category is per-device bounded on the agent; Truncated is set
	// when any category was capped.
	Ports      []DiscoveredPort      `msgpack:"ports,omitempty"`
	Services   []DiscoveredService   `msgpack:"services,omitempty"`
	DBEngines  []DiscoveredDbEngine  `msgpack:"db_engines,omitempty"`
	Containers []DiscoveredContainer `msgpack:"containers,omitempty"`
	Packages   []DiscoveredPackage   `msgpack:"packages,omitempty"`

	// SetMaintenanceMode (server → agent) / MaintenanceApplied (agent → server).
	// A pointer so a false value still serializes — omitempty would otherwise
	// drop it and break the byte-for-byte contract with Rust's always-present
	// field.
	Enabled *bool `msgpack:"enabled,omitempty"`
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

// ThresholdRule is one declarative edge threshold-alert rule (WS-19), evaluated
// locally by the agent. It mirrors the Rust ThresholdRule struct. Rules are
// tenant-scoped config pushed to the agent via PushAlertRules.
type ThresholdRule struct {
	ID          string          `msgpack:"id"`
	Metric      string          `msgpack:"metric"`
	Comparator  AlertComparator `msgpack:"comparator"`
	Threshold   float64         `msgpack:"threshold"`
	Clear       float64         `msgpack:"clear"`
	SustainSecs uint32          `msgpack:"sustain_secs"`
}

// AlertBreach is one currently-firing threshold-alert breach (WS-19), carried
// additively in an AgentHealthSummary. Investigation-aid only — no auto-notify.
type AlertBreach struct {
	RuleID string  `msgpack:"rule_id"`
	Metric string  `msgpack:"metric"`
	Value  float64 `msgpack:"value"`
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

// BackfillSample is one pre-rolled historical sample replayed during reconnect
// backfill. Central VM keeps avg only, so it carries the dimension, the original
// sample timestamp (seconds), and the averaged value for that bucket.
type BackfillSample struct {
	Name  string  `msgpack:"name"`
	TS    int64   `msgpack:"ts"`
	Value float64 `msgpack:"value"`
}

// HistoryPoint is one point in an on-demand deep-history pull of a single
// dimension: original timestamp (seconds) and value.
type HistoryPoint struct {
	TS    int64   `msgpack:"ts"`
	Value float64 `msgpack:"value"`
}

// DiscoveredPort is one listening network port discovered on the host (WS-16):
// transport, port number, and owning process basename only — never a bound
// address.
type DiscoveredPort struct {
	Proto   string `msgpack:"proto"`
	Port    uint16 `msgpack:"port"`
	Process string `msgpack:"process"`
}

// DiscoveredService is one host service (systemd unit / Windows service): its
// name and normalized run state.
type DiscoveredService struct {
	Name  string `msgpack:"name"`
	State string `msgpack:"state"`
}

// DiscoveredDbEngine is one database engine inferred from a listening port plus
// its owning process: engine family, best-effort version, and port — never a
// connection string or credential.
type DiscoveredDbEngine struct {
	Engine  string `msgpack:"engine"`
	Version string `msgpack:"version"`
	Port    uint16 `msgpack:"port"`
}

// DiscoveredContainer is one container discovered via a read-only local runtime:
// runtime, image reference, container name, and state.
type DiscoveredContainer struct {
	Runtime string `msgpack:"runtime"`
	Image   string `msgpack:"image"`
	Name    string `msgpack:"name"`
	State   string `msgpack:"state"`
}

// DiscoveredPackage is one installed OS package (dpkg/rpm / Windows registry):
// name and installed version.
type DiscoveredPackage struct {
	Name    string `msgpack:"name"`
	Version string `msgpack:"version"`
}
