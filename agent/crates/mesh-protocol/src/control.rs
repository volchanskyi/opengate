use crate::types::{AgentCapability, Permissions, SessionToken};
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
        signature: String,
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
}
