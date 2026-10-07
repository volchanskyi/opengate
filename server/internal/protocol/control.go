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

	// Offline reconnect-backfill messages.
	MsgRequestBackfillSlot  ControlMessageType = "RequestBackfillSlot"
	MsgGrantBackfill        ControlMessageType = "GrantBackfill"
	MsgDeferBackfill        ControlMessageType = "DeferBackfill"
	MsgMetricBackfillBatch  ControlMessageType = "MetricBackfillBatch"
	MsgMetricBackfillAck    ControlMessageType = "MetricBackfillAck"
	MsgRequestLocalHistory  ControlMessageType = "RequestLocalHistory"
	MsgLocalHistoryResponse ControlMessageType = "LocalHistoryResponse"

	// Auto-discovery inventory report.
	MsgDiscoveryReport ControlMessageType = "DiscoveryReport"

	// Server-to-agent threshold-alert ruleset push.
	MsgPushAlertRules ControlMessageType = "PushAlertRules"

	// Maintenance mode: server → agent toggle and the agent's applied-state report.
	MsgSetMaintenanceMode ControlMessageType = "SetMaintenanceMode"
	MsgMaintenanceApplied ControlMessageType = "MaintenanceApplied"

	// MsgAgentAlert is the only alert transport: one alert carries everything the device knows
	// about why it fired, since the server holds no high-resolution history.
	MsgAgentAlert ControlMessageType = "AgentAlert"
)

// AlertSeverity is how bad an alert is, serialized as the Rust AlertSeverity variant name.
// The set is closed so every stored value is one the incident view can render.
type AlertSeverity string

const (
	// AlertSeverityInfo is worth recording beside an incident, not worth raising
	// one for.
	AlertSeverityInfo AlertSeverity = "Info"
	// AlertSeverityWarning means something is wrong and a person should look.
	AlertSeverityWarning AlertSeverity = "Warning"
	// AlertSeverityCritical means something is broken now.
	AlertSeverityCritical AlertSeverity = "Critical"
)

const (
	// EvidenceCodec names the DEFLATE codec of AlertEvidence; the version in the name travels on
	// every alert so a later codec is additive.
	EvidenceCodec = "deflate-1"

	// MaxEvidenceBytes is the most compressed evidence an alert carries; the agent truncates to
	// fit and still sends the alert.
	MaxEvidenceBytes = 64 * 1024
)

// ValidAlertSeverity reports whether s is one of the three severities the
// contract defines.
func ValidAlertSeverity(s AlertSeverity) bool {
	switch s {
	case AlertSeverityInfo, AlertSeverityWarning, AlertSeverityCritical:
		return true
	default:
		return false
	}
}

// AlertComparator is the comparison direction of a threshold-alert rule, serialized as the
// Rust AlertComparator variant name.
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

// BackfillTier names the central VictoriaMetrics tier a backfill batch targets, serialized as
// the Rust BackfillTier variant name.
type BackfillTier string

const (
	// BackfillTierRecent60s carries 60 s windows rolled from local T0 → VM raw
	// tier, on the same grid live telemetry streams on.
	BackfillTierRecent60s BackfillTier = "Recent60s"
	// BackfillTierRollup1m carries 1 min points from local T1 → VM 1 min rollup.
	BackfillTierRollup1m BackfillTier = "Rollup1m"
	// BackfillTierRollup1h carries 1 hr points from local T2 → VM 1 hr rollup.
	BackfillTierRollup1h BackfillTier = "Rollup1h"
)

// ControlMessage is the control-plane envelope, encoded as a msgpack map keyed by the variant
// name to match the Rust enum.
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
	TenantID        string               `msgpack:"tenant_id,omitempty"`
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

	// Breaches and RuleCoverage ride an AgentHealthSummary (agent to server); AlertRules ride a
	// PushAlertRules (server to agent).
	Breaches     []AlertBreach   `msgpack:"breaches,omitempty"`
	AlertRules   []ThresholdRule `msgpack:"rules,omitempty"`
	RuleCoverage []RuleCoverage  `msgpack:"rule_coverage,omitempty"`
	// DeviceHourlyCeiling rides a PushAlertRules: the most alerts the machine may raise in a
	// rolling hour, enforced where the alerts are raised.
	DeviceHourlyCeiling uint32 `msgpack:"device_hourly_ceiling,omitempty"`

	// Reconnect-backfill scheduler and tiered replay.
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
	// SystemUUID is the SMBIOS UUID that resolves which device an AMT CIRA connection belongs to.
	// AMTAvailable is a pointer: a stated false survives omitempty; absent keeps the old value.
	SystemUUID   string `msgpack:"system_uuid,omitempty"`
	AMTAvailable *bool  `msgpack:"amt_available,omitempty"`
	AMTVersion   string `msgpack:"amt_version,omitempty"`

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
	// AvailableUnits lists the distinct emitting units (capped, sorted) for the unit dropdown;
	// empty for the agent's own files and for older agents.
	AvailableUnits []string `msgpack:"available_units,omitempty"`

	// DiscoveryReport: each category is bounded per device on the agent, and Truncated is set
	// when any category was capped.
	Ports      []DiscoveredPort      `msgpack:"ports,omitempty"`
	Services   []DiscoveredService   `msgpack:"services,omitempty"`
	DBEngines  []DiscoveredDbEngine  `msgpack:"db_engines,omitempty"`
	Containers []DiscoveredContainer `msgpack:"containers,omitempty"`
	Packages   []DiscoveredPackage   `msgpack:"packages,omitempty"`

	// SetMaintenanceMode (server to agent) and MaintenanceApplied (agent to server). A pointer
	// keeps a false value serialized, matching Rust's always-present field.
	Enabled *bool `msgpack:"enabled,omitempty"`

	// AgentAlert (agent to server). Severity and Backfilled are pointers so a stated value stays
	// distinct from an absent one.
	AlertID       string         `msgpack:"alert_id,omitempty"`
	RuleID        string         `msgpack:"rule_id,omitempty"`
	RuleVersion   uint32         `msgpack:"rule_version,omitempty"`
	Severity      *AlertSeverity `msgpack:"severity,omitempty"`
	Metric        string         `msgpack:"metric,omitempty"`
	Value         *float64       `msgpack:"value,omitempty"`
	WindowStartTS int64          `msgpack:"window_start_ts,omitempty"`
	WindowEndTS   int64          `msgpack:"window_end_ts,omitempty"`
	ObservedTS    int64          `msgpack:"observed_ts,omitempty"`
	Backfilled    *bool          `msgpack:"backfilled,omitempty"`
	// EvidenceCodec names how Evidence is compressed, e.g. "deflate-1", so a later codec is additive.
	EvidenceCodec string `msgpack:"evidence_codec,omitempty"`
	// Evidence is a compressed AlertEvidence. Empty is a legal alert: the device
	// had nothing to attach, which still says the machine is in trouble.
	Evidence []byte `msgpack:"evidence,omitempty"`
}

// RankedDim is one dimension the device's own correlation engine ranked at the
// moment an alert fired, and how badly it broke pattern.
type RankedDim struct {
	Dim   string  `msgpack:"dim"`
	Score float64 `msgpack:"score"`
}

// EvidenceSeries is one dimension's readings either side of the event at the device's own
// resolution, finer than the 60 s average central keeps.
type EvidenceSeries struct {
	Dim    string         `msgpack:"dim"`
	Points []HistoryPoint `msgpack:"points"`
}

// AlertEvidence is everything the device knows about why an alert fired, carried compressed in
// ControlMessage.Evidence and never fetched afterwards.
type AlertEvidence struct {
	Ranked     []RankedDim       `msgpack:"ranked"`
	Series     []EvidenceSeries  `msgpack:"series"`
	Processes  []EvidenceProcess `msgpack:"processes"`
	LogSamples []string          `msgpack:"log_samples"`
	// Truncated says whether the size cap cost this evidence anything, so
	// "nothing was dropped" and "nobody checked" never look alike.
	Truncated bool `msgpack:"truncated"`
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

// RulePredicate is how a rule derives the number it compares to its threshold, serialized as
// the Rust RulePredicate variant name.
type RulePredicate string

const (
	// RulePredicateInstant compares the reading itself, this second.
	RulePredicateInstant RulePredicate = "Instant"
	// RulePredicateRate compares change per second across the rule's window, the shape of a
	// resource getting worse.
	RulePredicateRate RulePredicate = "Rate"
	// RulePredicateWindowMax compares the largest reading in the window. A
	// minute's average hides a five-second freeze; its maximum does not.
	RulePredicateWindowMax RulePredicate = "WindowMax"
	// RulePredicateWindowMean compares the mean reading over the window, smoothing spikes.
	RulePredicateWindowMean RulePredicate = "WindowMean"
)

// RuleTerm is one extra condition a rule requires at the same instant as its own, mirroring the
// Rust RuleTerm struct; sustain and the firing state belong to the rule.
type RuleTerm struct {
	Metric     string          `msgpack:"metric"`
	Comparator AlertComparator `msgpack:"comparator"`
	Threshold  float64         `msgpack:"threshold"`
	Clear      float64         `msgpack:"clear"`
	Predicate  RulePredicate   `msgpack:"predicate"`
	WindowSecs uint32          `msgpack:"window_secs"`
}

// ThresholdRule is one declarative threshold-alert rule the agent evaluates locally, mirroring
// the Rust ThresholdRule struct; rules are tenant-scoped config pushed via PushAlertRules.
type ThresholdRule struct {
	ID string `msgpack:"id"`
	// Version is the revision of this definition, part of an alert's identity (device, rule,
	// revision, window start) and always emitted.
	Version uint32 `msgpack:"version"`
	// Severity is how bad this rule's alerts are; the machine states it on every alert it raises.
	Severity    AlertSeverity   `msgpack:"severity"`
	Metric      string          `msgpack:"metric"`
	Comparator  AlertComparator `msgpack:"comparator"`
	Threshold   float64         `msgpack:"threshold"`
	Clear       float64         `msgpack:"clear"`
	SustainSecs uint32          `msgpack:"sustain_secs"`
	Predicate   RulePredicate   `msgpack:"predicate"`
	WindowSecs  uint32          `msgpack:"window_secs"`
	// All is omitempty because the agent's decoder defaults a missing key but
	// would reject an explicit nil.
	All []RuleTerm `msgpack:"all,omitempty"`
}

// AlertBreach is one currently-firing threshold-alert breach, carried in an AgentHealthSummary
// as an investigation aid.
type AlertBreach struct {
	RuleID string  `msgpack:"rule_id"`
	Metric string  `msgpack:"metric"`
	Value  float64 `msgpack:"value"`
}

// RuleCoverageState is what one rule does on one device, serialized as the Rust
// RuleCoverageState variant name; a device reporting none is unknown.
type RuleCoverageState string

const (
	// RuleCoverageActive means the rule is being evaluated on this device.
	RuleCoverageActive RuleCoverageState = "Active"
	// RuleCoverageUnsupported means the rule cannot be evaluated here: unknown metric, predicate
	// outside the grammar's bounds, or a reading this host cannot take.
	RuleCoverageUnsupported RuleCoverageState = "Unsupported"
	// RuleCoverageThrottled means the rule cost more than its allowance and the device stopped
	// running it; the state describes the rule, not the host.
	RuleCoverageThrottled RuleCoverageState = "Throttled"
)

// RuleCoverage is one rule's state on the device reporting it, carried
// additively in an AgentHealthSummary.
type RuleCoverage struct {
	RuleID string            `msgpack:"rule_id"`
	State  RuleCoverageState `msgpack:"state"`
}

// EvidenceProcess is one process running when an alert fired, as the device measured it.
type EvidenceProcess struct {
	Rank        uint32  `msgpack:"rank"`
	Basename    string  `msgpack:"basename"`
	CmdlineHash *string `msgpack:"cmdline_hash,omitempty"`
	PID         uint32  `msgpack:"pid"`
	// CPUShare is the share of the whole host's processors, 0–100; nil when the device could
	// not measure it.
	CPUShare *float64 `msgpack:"cpu_share,omitempty"`
	// Mem is resident memory, in bytes.
	Mem float64 `msgpack:"mem"`
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
	TenantID        string              `msgpack:"tenant_id"`
	NodeAnomalyRate float64             `msgpack:"node_anomaly_rate"`
	PerFamilyRates  []FamilyAnomalyRate `msgpack:"per_family_rates"`
	RecentBitmask   []byte              `msgpack:"recent_bitmask"`
	SamplerVersion  string              `msgpack:"sampler_ver"`
	ModelVersion    string              `msgpack:"model_ver"`
}

// BackfillSample is one pre-rolled historical sample replayed during reconnect backfill: the
// dimension, the original timestamp in seconds, and the bucket's average.
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

// DiscoveredPort is one listening port on the host: transport, port number and owning process
// basename, never a bound address.
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

// DiscoveredDbEngine is one database engine inferred from a listening port and its process:
// family, best-effort version and port, never a connection string or credential.
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
