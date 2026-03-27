//! IPC protocol types for communication between `mesh-agent` and `mesh-agent-tray`.
//!
//! Uses JSON-over-newline encoding on a Unix domain socket.
//! Each message is a single JSON object terminated by `\n`.

use serde::{Deserialize, Serialize};

/// Request sent from the tray to the agent.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
#[non_exhaustive]
pub enum TrayRequest {
    /// Query current agent status.
    Status,
    /// Request agent restart (exit code 42 → systemd restarts).
    Restart,
    /// Ask the agent to check for updates with the server.
    CheckUpdate,
    /// Request a short-lived chat authentication token.
    RequestChatToken,
    /// Request full agent build/runtime info.
    GetInfo,
    /// Request recent log lines from the agent's log file.
    GetLogs {
        /// Number of recent lines to return.
        lines: u32,
    },
}

/// Response sent from the agent to the tray.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
#[non_exhaustive]
pub enum TrayResponse {
    /// Current agent status.
    Status {
        connected: bool,
        version: String,
        server_addr: String,
        uptime_secs: u64,
    },
    /// Acknowledgement that restart was accepted.
    RestartAck,
    /// Update check status.
    UpdateStatus {
        status: UpdateState,
        version: String,
    },
    /// Chat authentication token for webview.
    ChatToken {
        url: String,
        token: String,
        expires_at: String,
    },
    /// Full agent build and runtime information.
    Info {
        version: String,
        device_id: String,
        hostname: String,
        os: String,
        arch: String,
        server_addr: String,
        connected: bool,
        uptime_secs: u64,
        log_path: String,
    },
    /// Recent log lines.
    Logs { lines: Vec<String> },
    /// Generic error response.
    Error { message: String },
}

/// Push event sent from agent to tray (unsolicited).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
#[non_exhaustive]
pub enum TrayEvent {
    /// Agent connection status changed.
    ConnectionChanged { connected: bool },
    /// Update download progress.
    UpdateProgress { percent: u8, version: String },
}

/// State of an update check/download.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[non_exhaustive]
pub enum UpdateState {
    Checking,
    Downloading,
    NoUpdate,
    Applied,
    Failed,
}

/// Encode a message as a JSON line (terminated by `\n`).
pub fn encode_line<T: Serialize>(msg: &T) -> Result<Vec<u8>, serde_json::Error> {
    let mut buf = serde_json::to_vec(msg)?;
    buf.push(b'\n');
    Ok(buf)
}

/// Decode a JSON line into a message.
pub fn decode_line<'a, T: Deserialize<'a>>(line: &'a [u8]) -> Result<T, serde_json::Error> {
    serde_json::from_slice(line)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_encode_decode_request_status() {
        let req = TrayRequest::Status;
        let encoded = encode_line(&req).unwrap();
        let decoded: TrayRequest = decode_line(&encoded).unwrap();
        assert_eq!(req, decoded);
    }

    #[test]
    fn test_encode_decode_request_get_logs() {
        let req = TrayRequest::GetLogs { lines: 500 };
        let encoded = encode_line(&req).unwrap();
        let decoded: TrayRequest = decode_line(&encoded).unwrap();
        assert_eq!(req, decoded);
    }

    #[test]
    fn test_encode_decode_response_status() {
        let resp = TrayResponse::Status {
            connected: true,
            version: "0.15.4".to_string(),
            server_addr: "opengate.example.com:4433".to_string(),
            uptime_secs: 3600,
        };
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }

    #[test]
    fn test_encode_decode_response_info() {
        let resp = TrayResponse::Info {
            version: "0.15.4".to_string(),
            device_id: "abc-123".to_string(),
            hostname: "workstation".to_string(),
            os: "Ubuntu 24.04".to_string(),
            arch: "x86_64".to_string(),
            server_addr: "server:4433".to_string(),
            connected: true,
            uptime_secs: 86400,
            log_path: "/var/log/mesh-agent/agent.log".to_string(),
        };
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }

    #[test]
    fn test_encode_decode_response_error() {
        let resp = TrayResponse::Error {
            message: "not connected".to_string(),
        };
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }

    #[test]
    fn test_encode_decode_event() {
        let evt = TrayEvent::ConnectionChanged { connected: false };
        let encoded = encode_line(&evt).unwrap();
        let decoded: TrayEvent = decode_line(&encoded).unwrap();
        assert_eq!(evt, decoded);
    }

    #[test]
    fn test_encode_decode_update_progress() {
        let evt = TrayEvent::UpdateProgress {
            percent: 45,
            version: "0.16.0".to_string(),
        };
        let encoded = encode_line(&evt).unwrap();
        let decoded: TrayEvent = decode_line(&encoded).unwrap();
        assert_eq!(evt, decoded);
    }

    #[test]
    fn test_encode_decode_chat_token() {
        let resp = TrayResponse::ChatToken {
            url: "https://example.com/chat".to_string(),
            token: "abc123".to_string(),
            expires_at: "2026-03-26T12:00:00Z".to_string(),
        };
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }

    #[test]
    fn test_encode_decode_restart_ack() {
        let resp = TrayResponse::RestartAck;
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }

    #[test]
    fn test_encode_decode_update_status() {
        let resp = TrayResponse::UpdateStatus {
            status: UpdateState::NoUpdate,
            version: "0.15.4".to_string(),
        };
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }

    #[test]
    fn test_encode_line_ends_with_newline() {
        let req = TrayRequest::Status;
        let encoded = encode_line(&req).unwrap();
        assert_eq!(*encoded.last().unwrap(), b'\n');
    }

    #[test]
    fn test_decode_invalid_json() {
        let result: Result<TrayRequest, _> = decode_line(b"not json");
        assert!(result.is_err());
    }

    #[test]
    fn test_decode_wrong_type() {
        // Valid JSON but wrong type tag
        let result: Result<TrayRequest, _> = decode_line(b"{\"type\":\"Unknown\"}");
        assert!(result.is_err());
    }

    #[test]
    fn test_all_update_states_roundtrip() {
        for state in [
            UpdateState::Checking,
            UpdateState::Downloading,
            UpdateState::NoUpdate,
            UpdateState::Applied,
            UpdateState::Failed,
        ] {
            let resp = TrayResponse::UpdateStatus {
                status: state,
                version: "1.0.0".to_string(),
            };
            let encoded = encode_line(&resp).unwrap();
            let decoded: TrayResponse = decode_line(&encoded).unwrap();
            assert_eq!(resp, decoded);
        }
    }

    #[test]
    fn test_logs_response_roundtrip() {
        let resp = TrayResponse::Logs {
            lines: vec![
                "2026-03-26 INFO connected".to_string(),
                "2026-03-26 DEBUG heartbeat".to_string(),
            ],
        };
        let encoded = encode_line(&resp).unwrap();
        let decoded: TrayResponse = decode_line(&encoded).unwrap();
        assert_eq!(resp, decoded);
    }
}
