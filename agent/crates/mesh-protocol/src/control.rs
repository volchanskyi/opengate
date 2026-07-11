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

    /// Unknown future control message. Agents ignore this and keep the
    /// control stream alive; malformed frames still fail before this point.
    #[serde(other)]
    Unknown,
}
