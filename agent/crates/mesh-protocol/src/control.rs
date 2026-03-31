use crate::types::{
    AgentCapability, FileEntry, KeyCode, MouseButton, NetworkInterface, Permissions, SessionToken,
};
use serde::{Deserialize, Serialize};

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
}
