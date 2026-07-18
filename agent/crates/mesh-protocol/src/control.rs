use crate::types::{
    AgentCapability, FileEntry, KeyCode, LogEntry, MouseButton, NetworkInterface, Permissions,
    SessionToken,
};
use serde::{Deserialize, Serialize};

/// Per-family anomaly rate inside an Edge Sentinel health summary.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FamilyAnomalyRate {
    pub family: String,
    pub rate: f64,
}

/// Averaged metric dimension in an Edge Sentinel metric window.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct MetricDim {
    pub name: String,
    pub avg: f64,
}

/// Comparison direction for a WS-19 declarative threshold-alert rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[non_exhaustive]
pub enum AlertComparator {
    /// Breaches while the metric is strictly greater than the threshold.
    Gt,
    /// Breaches while the metric is strictly less than the threshold.
    Lt,
    /// Breaches while the metric is greater than or equal to the threshold.
    Gte,
    /// Breaches while the metric is less than or equal to the threshold.
    Lte,
}

/// One declarative edge threshold-alert rule (WS-19), evaluated locally against a
/// sampler dimension every window. A breach must sustain `sustain_secs`
/// continuously before it fires, and `clear` adds hysteresis so a value dithering
/// around `threshold` does not flap. Rules are tenant-scoped config pushed to the
/// agent; a resulting breach is investigation-aid only until the FPR soak.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ThresholdRule {
    /// Stable rule id, used to attribute a breach and to preserve evaluation
    /// state across an identical rule re-push.
    pub id: String,
    /// Watched sampler dimension: `"cpu.total"`, `"mem.used"`, or `"disk.used"`
    /// (percent gauges). An unrecognized metric never fires.
    pub metric: String,
    /// Comparison direction.
    pub comparator: AlertComparator,
    /// Fire boundary.
    pub threshold: f64,
    /// Hysteresis clear boundary on the safe side of `threshold`; equal to
    /// `threshold` disables hysteresis.
    pub clear: f64,
    /// Seconds the breach must hold continuously before it fires.
    pub sustain_secs: u32,
}

/// One currently-firing threshold-alert breach (WS-19), carried additively in an
/// `AgentHealthSummary`. Investigation-aid only — no auto-notify.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AlertBreach {
    /// Id of the [`ThresholdRule`] that fired.
    pub rule_id: String,
    /// Watched dimension, echoed for legibility without a rule-table join.
    pub metric: String,
    /// Metric value at the evaluation that reported the breach.
    pub value: f64,
}

/// Sanitized process sample row for Edge Sentinel reporting.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProcessReportEntry {
    pub rank: u32,
    pub basename: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cmdline_hash: Option<String>,
    pub pid: u32,
    pub cpu: f64,
    pub mem: f64,
}

/// Bounded health summary point returned for read-back requests.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct HealthSummary {
    pub ts: i64,
    pub org_id: String,
    pub node_anomaly_rate: f64,
    #[serde(default)]
    pub per_family_rates: Vec<FamilyAnomalyRate>,
    #[serde(default, with = "serde_bytes")]
    pub recent_bitmask: Vec<u8>,
    pub sampler_ver: String,
    pub model_ver: String,
}

/// Which central VictoriaMetrics tier a reconnect-backfill batch targets. The
/// agent maps each local WS-14b tier to the matching central tier; full-res 1 s
/// raw is never pushed (it is reachable only via an on-demand deep-history pull).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[non_exhaustive]
pub enum BackfillTier {
    /// 10 s windows rolled from local T0 (1 s) → VM raw tier.
    #[default]
    Raw10s,
    /// 1 min points from local T1 → VM 1 min rollup.
    Rollup1m,
    /// 1 hr points from local T2 → VM 1 hr rollup.
    Rollup1h,
}

/// One pre-rolled historical sample replayed during reconnect backfill. Central
/// VM keeps `avg` only, so a sample carries just its dimension, original
/// timestamp (seconds), and averaged value for that bucket.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct BackfillSample {
    pub name: String,
    pub ts: i64,
    pub value: f64,
}

/// One point in an on-demand deep-history pull of a single dimension. Unlike a
/// backfill batch (which spans many dims), a history response is scoped to one
/// dimension, so a point needs only its timestamp (seconds) and value.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct HistoryPoint {
    pub ts: i64,
    pub value: f64,
}

/// One listening network port discovered on the host (WS-16). Read-only: the
/// transport, the port number, and the owning process basename only — never a
/// bound address that could leak internal topology beyond the port itself.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredPort {
    /// Transport, lowercase: `"tcp"` or `"udp"`.
    pub proto: String,
    /// Listening port number.
    pub port: u16,
    /// Basename of the owning process, or `""` when it cannot be resolved
    /// non-intrusively.
    pub process: String,
}

/// One host service discovered on the endpoint (WS-16) — a systemd unit on Linux
/// or a Windows service. Carries the unit name and its run state only.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredService {
    /// Unit / service name (e.g. `"nginx.service"`, `"Spooler"`).
    pub name: String,
    /// Normalized run state (e.g. `"running"`, `"exited"`, `"failed"`,
    /// `"stopped"`).
    pub state: String,
}

/// One database engine inferred from a listening port plus its owning process
/// (WS-16). Engine family, best-effort version, and port only — never a
/// connection string or credential.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredDbEngine {
    /// Engine family, lowercase (e.g. `"postgres"`, `"mysql"`, `"mongodb"`,
    /// `"redis"`).
    pub engine: String,
    /// Best-effort version string, or `""` when it is not determinable without
    /// an intrusive query.
    pub version: String,
    /// Port the engine listens on.
    pub port: u16,
}

/// One container discovered via a read-only local runtime (WS-16). Runtime,
/// image reference, container name, and state only.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredContainer {
    /// Runtime, lowercase: `"docker"`, `"podman"`, or `"containerd"`.
    pub runtime: String,
    /// Image reference (repository[:tag]).
    pub image: String,
    /// Container name.
    pub name: String,
    /// Normalized state (e.g. `"running"`, `"exited"`, `"created"`).
    pub state: String,
}

/// One installed OS package discovered on the host (WS-16) — dpkg/rpm on Linux
/// or the Windows package registry. Name and version only.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredPackage {
    /// Package name.
    pub name: String,
    /// Installed version string.
    pub version: String,
}

/// All control messages exchanged between agent and server.
/// Uses internally tagged representation so msgpack output matches Go's flat struct:
/// {"type": "AgentRegister", "capabilities": [...], "hostname": "...", "os": "..."}
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
#[non_exhaustive]
pub enum ControlMessage {
    // Agent → Server
    AgentRegister {
        capabilities: Vec<AgentCapability>,
        hostname: String,
        os: String,
        arch: String,
        version: String,
    },
    AgentHeartbeat {
        timestamp: i64,
    },
    AgentHealthSummary {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        org_id: String,
        #[serde(default)]
        node_anomaly_rate: f64,
        #[serde(default)]
        per_family_rates: Vec<FamilyAnomalyRate>,
        #[serde(default, with = "serde_bytes")]
        recent_bitmask: Vec<u8>,
        #[serde(default)]
        sampler_ver: String,
        #[serde(default)]
        model_ver: String,
        /// Threshold-alert breaches firing when this summary was built (WS-19).
        /// Additive: an older decoder ignores it; a newer one defaults it empty.
        #[serde(default)]
        breaches: Vec<AlertBreach>,
    },
    AgentMetricWindow {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        org_id: String,
        #[serde(default)]
        dims: Vec<MetricDim>,
    },
    ProcessReport {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        org_id: String,
        #[serde(default)]
        top_n: Vec<ProcessReportEntry>,
    },
    SessionAccept {
        token: SessionToken,
        relay_url: String,
    },
    SessionReject {
        token: SessionToken,
        reason: String,
    },

    // Server → Agent
    SessionRequest {
        token: SessionToken,
        relay_url: String,
        permissions: Permissions,
    },
    AgentUpdate {
        version: String,
        url: String,
        #[serde(default)]
        sha256: String,
        signature: String,
    },
    /// Agent acknowledges an update attempt (success or failure).
    AgentUpdateAck {
        version: String,
        success: bool,
        error: String,
    },

    // Bidirectional
    RelayReady,
    SwitchToWebRTC {
        sdp_offer: String,
    },
    SwitchAck,
    IceCandidate {
        candidate: String,
        mid: String,
    },

    // Input (browser → agent via relay)
    MouseMove {
        x: u16,
        y: u16,
    },
    MouseClick {
        button: MouseButton,
        pressed: bool,
        x: u16,
        y: u16,
    },
    KeyPress {
        key: KeyCode,
        pressed: bool,
    },
    TerminalResize {
        cols: u16,
        rows: u16,
    },

    // File operations
    FileListRequest {
        path: String,
    },
    FileListResponse {
        path: String,
        entries: Vec<FileEntry>,
    },
    FileListError {
        path: String,
        error: String,
    },
    FileDownloadRequest {
        path: String,
    },
    FileUploadRequest {
        path: String,
        total_size: u64,
    },

    // Chat
    ChatMessage {
        text: String,
        sender: String,
    },

    // Agent → Server: request an update check.
    RequestUpdate,

    // Server → Agent: response to RequestUpdate.
    UpdateCheckResponse {
        available: bool,
        version: String,
        url: String,
        sha256: String,
        signature: String,
    },

    // Agent → Server: request a short-lived chat authentication token.
    RequestChatToken {
        device_id: String,
    },

    // Server → Agent: chat token response.
    ChatTokenResponse {
        url: String,
        token: String,
        expires_at: String,
    },

    // Device lifecycle
    /// Server notifies agent that its device has been deleted.
    /// Agent should clean up and exit.
    AgentDeregistered {
        reason: String,
    },

    /// Server requests agent to restart (exit code 42, systemd auto-restarts).
    RestartAgent {
        reason: String,
    },

    /// Server requests the agent to collect and send hardware inventory.
    RequestHardwareReport,

    /// Agent reports hardware inventory to the server.
    HardwareReport {
        cpu_model: String,
        cpu_cores: u32,
        ram_total_mb: u64,
        disk_total_mb: u64,
        disk_free_mb: u64,
        network_interfaces: Vec<NetworkInterface>,
    },

    /// Agent reports a hardware collection error.
    HardwareReportError {
        error: String,
    },

    /// Server requests the agent to collect and send log entries.
    RequestDeviceLogs {
        #[serde(default)]
        log_level: String,
        #[serde(default)]
        time_from: String,
        #[serde(default)]
        time_to: String,
        #[serde(default)]
        search: String,
        #[serde(default)]
        log_offset: u32,
        #[serde(default)]
        log_limit: u32,
        /// Host log source to query ("self", "journald", "windows"). Empty
        /// selects the agent's own files, so older servers stay compatible.
        #[serde(default)]
        source: String,
        /// Structured filter on the emitting unit (systemd unit or Windows
        /// provider). Empty matches every unit.
        #[serde(default)]
        unit: String,
    },

    /// Agent responds with log entries.
    DeviceLogsResponse {
        log_entries: Vec<LogEntry>,
        total_count: u32,
        has_more: bool,
    },

    /// Agent reports a log retrieval error.
    DeviceLogsError {
        error: String,
    },

    /// Server asks the agent for its bounded recent health summary window.
    RequestHealthWindow {
        #[serde(default)]
        since_ts: i64,
        #[serde(default)]
        limit: u32,
    },

    /// Agent responds with a bounded recent health summary window.
    HealthWindowResponse {
        #[serde(default)]
        summaries: Vec<HealthSummary>,
    },

    /// Agent → Server: request an admission slot to drain persisted history.
    /// Carries backlog hints (pending sample count, oldest pending timestamp)
    /// so the server scheduler can prioritize and age fairly. Because local
    /// data is durable, this request can be deferred without data loss.
    RequestBackfillSlot {
        #[serde(default)]
        pending_samples: u64,
        #[serde(default)]
        oldest_ts: i64,
    },

    /// Server → Agent: admission granted. `rate` bounds the drain to N
    /// samples/sec; the grant expires at `deadline` (unix seconds) and must be
    /// re-requested afterwards. Gated by the Backfill capability.
    GrantBackfill {
        #[serde(default)]
        rate: u32,
        #[serde(default)]
        deadline: i64,
    },

    /// Server → Agent: admission deferred; retry after `retry_after` seconds
    /// (the agent adds jitter). Durable local data means deferral never loses
    /// samples. Gated by the Backfill capability.
    DeferBackfill {
        #[serde(default)]
        retry_after: u32,
    },

    /// Agent → Server: a batch of pre-rolled historical samples for one tier,
    /// written to the matching VM tier at their original timestamps (never
    /// through live stream-aggregation). `cursor` is the newest bucket
    /// timestamp in this batch; the agent advances its durable per-tier
    /// watermark only after the matching `MetricBackfillAck`.
    MetricBackfillBatch {
        #[serde(default)]
        tier: BackfillTier,
        #[serde(default)]
        samples: Vec<BackfillSample>,
        #[serde(default)]
        cursor: i64,
    },

    /// Server → Agent: durability ack for a persisted backfill batch. The agent
    /// advances its durable per-tier watermark to `cursor` on receipt. Gated by
    /// the Backfill capability.
    MetricBackfillAck {
        #[serde(default)]
        tier: BackfillTier,
        #[serde(default)]
        cursor: i64,
    },

    /// Server → Agent: on-demand pull of deep/full-res history for one dimension
    /// over a bounded window. Brokered from an authenticated, admin-gated API
    /// call; single-host, never a fan-out. Gated by the Backfill capability.
    RequestLocalHistory {
        #[serde(default)]
        dim: String,
        #[serde(default)]
        from_ts: i64,
        #[serde(default)]
        to_ts: i64,
        #[serde(default)]
        max_points: u32,
    },

    /// Agent → Server: bounded response to `RequestLocalHistory` for one
    /// dimension. `truncated` is set when the window held more than `max_points`
    /// samples and the response was capped.
    LocalHistoryResponse {
        #[serde(default)]
        dim: String,
        #[serde(default)]
        points: Vec<HistoryPoint>,
        #[serde(default)]
        truncated: bool,
    },

    /// Agent → Server: a tenant-scoped auto-discovery profile of the host
    /// (WS-16) — listening ports, host services, database engines, containers,
    /// and installed packages. Non-intrusive, read-only, per-category bounded;
    /// `truncated` is set when any category was capped. Carries no secrets
    /// (engine/port/version only, never connection strings or credentials). The
    /// server assigns the authoritative org, so the agent leaves `org_id` empty.
    /// Gated by the Discovery capability.
    DiscoveryReport {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        org_id: String,
        #[serde(default)]
        ports: Vec<DiscoveredPort>,
        #[serde(default)]
        services: Vec<DiscoveredService>,
        #[serde(default)]
        db_engines: Vec<DiscoveredDbEngine>,
        #[serde(default)]
        containers: Vec<DiscoveredContainer>,
        #[serde(default)]
        packages: Vec<DiscoveredPackage>,
        #[serde(default)]
        truncated: bool,
    },

    /// Server → Agent: replace the agent's active threshold-alert ruleset (WS-19)
    /// with this tenant-scoped set. The server sends only the connecting agent's
    /// authoritative-org rules; the agent evaluates them locally each window and
    /// carries any breach in `AgentHealthSummary`. Gated by the ThresholdAlerts
    /// capability.
    PushAlertRules {
        #[serde(default)]
        rules: Vec<ThresholdRule>,
    },

    /// Server → Agent: set the device's maintenance state. In maintenance the
    /// agent stops telemetry/discovery/log collection and suppresses alert-breach
    /// evaluation while keeping the control channel and remote-management paths
    /// live; leaving maintenance resumes collection. Server-authoritative and
    /// pushed on connect and on every change; `enabled` defaults to `false`
    /// (Active) so an absent field decodes as not-in-maintenance.
    SetMaintenanceMode {
        #[serde(default)]
        enabled: bool,
    },

    /// Agent → Server: applied-state report for maintenance mode. The agent
    /// echoes the state it actually reconciled to after a `SetMaintenanceMode`
    /// so the server can track applied vs. desired and surface a device that has
    /// not yet converged.
    MaintenanceApplied {
        #[serde(default)]
        enabled: bool,
    },

    /// Unknown future control message. Agents ignore this and keep the
    /// control stream alive; malformed frames still fail before this point.
    #[serde(other)]
    Unknown,
}
