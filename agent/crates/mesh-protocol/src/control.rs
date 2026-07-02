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
        ts: i64,
        org_id: String,
        node_anomaly_rate: f64,
        #[serde(default)]
        per_family_rates: Vec<FamilyAnomalyRate>,
        #[serde(default, with = "serde_bytes")]
        recent_bitmask: Vec<u8>,
        sampler_ver: String,
        model_ver: String,
    },
    AgentMetricWindow {
        ts: i64,
        org_id: String,
        #[serde(default)]
        dims: Vec<MetricDim>,
    },
    ProcessReport {
        ts: i64,
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

    /// Unknown future control message. Agents ignore this and keep the
    /// control stream alive; malformed frames still fail before this point.
    #[serde(other)]
    Unknown,
}
