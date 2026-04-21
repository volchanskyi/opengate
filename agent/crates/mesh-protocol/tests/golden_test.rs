//! Golden file tests for cross-language protocol compatibility.
//!
//! When GENERATE_GOLDEN=1, generates golden files to testdata/golden/.
//! Otherwise, reads and verifies existing golden files.

use mesh_protocol::*;
use serde::Serialize;
use std::path::PathBuf;

fn golden_dir() -> PathBuf {
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("../../../testdata/golden");
    path
}

fn should_generate() -> bool {
    std::env::var("GENERATE_GOLDEN").is_ok_and(|v| v == "1")
}

/// Write a golden file or verify it matches.
fn golden_check(name: &str, data: &[u8]) {
    let path = golden_dir().join(name);
    if should_generate() {
        std::fs::create_dir_all(golden_dir()).unwrap();
        std::fs::write(&path, data).unwrap();
        eprintln!("Generated golden file: {}", path.display());
    } else {
        let expected = std::fs::read(&path).unwrap_or_else(|e| {
            panic!(
                "Golden file {} not found. Run with GENERATE_GOLDEN=1 first. Error: {}",
                path.display(),
                e
            )
        });
        assert_eq!(
            data,
            &expected[..],
            "Golden file mismatch: {}",
            path.display()
        );
    }
}

#[test]
fn golden_control_frame_agent_register() {
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![AgentCapability::RemoteDesktop, AgentCapability::Terminal],
        hostname: "golden-test-host".to_string(),
        os: "linux".to_string(),
        arch: "amd64".to_string(),
        version: "0.1.0".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_register.bin", &encoded);
}

#[test]
fn golden_control_frame_heartbeat() {
    let msg = ControlMessage::AgentHeartbeat {
        timestamp: 1700000000,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_heartbeat.bin", &encoded);
}

#[test]
fn golden_control_frame_relay_ready() {
    let msg = ControlMessage::RelayReady;
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_relay_ready.bin", &encoded);
}

#[test]
fn golden_desktop_frame() {
    let frame = Frame::Desktop(DesktopFrame {
        sequence: 42,
        x: 10,
        y: 20,
        width: 1920,
        height: 1080,
        encoding: FrameEncoding::Zstd,
        data: vec![0xDE, 0xAD, 0xBE, 0xEF],
    });
    let encoded = frame.encode().unwrap();
    golden_check("desktop_frame.bin", &encoded);
}

#[test]
fn golden_desktop_frame_jpeg() {
    let frame = Frame::Desktop(DesktopFrame {
        sequence: 99,
        x: 0,
        y: 0,
        width: 1920,
        height: 1080,
        encoding: FrameEncoding::Jpeg,
        data: vec![0xFF, 0xD8, 0xFF, 0xE0],
    });
    let encoded = frame.encode().unwrap();
    golden_check("desktop_frame_jpeg.bin", &encoded);
}

#[test]
fn golden_ping_pong() {
    let ping = Frame::Ping.encode().unwrap();
    let pong = Frame::Pong.encode().unwrap();
    golden_check("ping.bin", &ping);
    golden_check("pong.bin", &pong);
}

#[test]
fn golden_control_frame_switch_to_webrtc() {
    let msg = ControlMessage::SwitchToWebRTC {
        sdp_offer: "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_switch_to_webrtc.bin", &encoded);
}

#[test]
fn golden_control_frame_ice_candidate() {
    let msg = ControlMessage::IceCandidate {
        candidate: "candidate:1 1 UDP 2130706431 192.168.1.1 50000 typ host".to_string(),
        mid: "0".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_ice_candidate.bin", &encoded);
}

#[test]
fn golden_control_frame_switch_ack() {
    let msg = ControlMessage::SwitchAck;
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_switch_ack.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_update_ack() {
    let msg = ControlMessage::AgentUpdateAck {
        version: "1.2.3".to_string(),
        success: true,
        error: "".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_update_ack.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_deregistered() {
    let msg = ControlMessage::AgentDeregistered {
        reason: "device deleted by administrator".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_deregistered.bin", &encoded);
}

#[test]
fn golden_control_frame_restart_agent() {
    let msg = ControlMessage::RestartAgent {
        reason: "restart requested from web UI".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_restart_agent.bin", &encoded);
}

#[test]
fn golden_control_frame_request_hardware_report() {
    let msg = ControlMessage::RequestHardwareReport;
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_hardware_report.bin", &encoded);
}

#[test]
fn golden_control_frame_hardware_report() {
    let msg = ControlMessage::HardwareReport {
        cpu_model: "Intel Core i7-12700K".to_string(),
        cpu_cores: 12,
        ram_total_mb: 32768,
        disk_total_mb: 512000,
        disk_free_mb: 256000,
        network_interfaces: vec![NetworkInterface {
            name: "eth0".to_string(),
            mac: "00:11:22:33:44:55".to_string(),
            ipv4: vec!["192.168.1.100".to_string()],
            ipv6: vec!["fe80::1".to_string()],
        }],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_hardware_report.bin", &encoded);
}

#[test]
fn golden_control_frame_hardware_report_error() {
    let msg = ControlMessage::HardwareReportError {
        error: "failed to read system info".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_hardware_report_error.bin", &encoded);
}

#[test]
fn golden_control_frame_request_device_logs() {
    let msg = ControlMessage::RequestDeviceLogs {
        log_level: "WARN".to_string(),
        time_from: "2026-04-01T00:00:00Z".to_string(),
        time_to: "2026-04-01T23:59:59Z".to_string(),
        search: "connection".to_string(),
        log_offset: 0,
        log_limit: 100,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_request_device_logs.bin", &encoded);
}

#[test]
fn golden_control_frame_device_logs_response() {
    let msg = ControlMessage::DeviceLogsResponse {
        log_entries: vec![
            LogEntry {
                timestamp: "2026-04-01T12:00:00.000000Z".to_string(),
                level: "WARN".to_string(),
                target: "mesh_agent::connection".to_string(),
                message: "slow heartbeat detected".to_string(),
            },
            LogEntry {
                timestamp: "2026-04-01T12:00:01.000000Z".to_string(),
                level: "ERROR".to_string(),
                target: "mesh_agent::connection".to_string(),
                message: "connection lost".to_string(),
            },
        ],
        total_count: 42,
        has_more: true,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_device_logs_response.bin", &encoded);
}

#[test]
fn golden_control_frame_device_logs_error() {
    let msg = ControlMessage::DeviceLogsError {
        error: "log directory not found".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_device_logs_error.bin", &encoded);
}

#[test]
fn golden_handshake_server_hello() {
    let msg = HandshakeMessage::ServerHello {
        nonce: [0xAA; 32],
        cert_hash: [0xBB; 48],
    };
    let encoded = msg.encode_binary();
    golden_check("handshake_server_hello.bin", &encoded);
}

// --- Phase A: cross-boundary ControlMessage goldens ---
//
// Every variant below is encoded by Rust and decoded by Go in production.
// The corresponding Go verifier lives in server/internal/protocol/golden_test.go
// and asserts full struct fidelity — not just err == nil.

const GOLDEN_SESSION_TOKEN: &str =
    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";

#[test]
fn golden_control_frame_session_accept() {
    let msg = ControlMessage::SessionAccept {
        token: session_token(),
        relay_url: "wss://relay.example.com/relay".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_session_accept.bin", &encoded);
}

#[test]
fn golden_control_frame_session_reject() {
    let msg = ControlMessage::SessionReject {
        token: session_token(),
        reason: "agent is busy".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_session_reject.bin", &encoded);
}

#[test]
fn golden_control_frame_session_request() {
    let msg = ControlMessage::SessionRequest {
        token: session_token(),
        relay_url: "wss://relay.example.com/relay".to_string(),
        permissions: Permissions {
            desktop: true,
            terminal: true,
            file_read: true,
            file_write: false,
            input: true,
        },
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_session_request.bin", &encoded);
}

#[test]
fn golden_control_frame_agent_update() {
    let msg = ControlMessage::AgentUpdate {
        version: "1.2.3".to_string(),
        url: "https://updates.example.com/agent-1.2.3-linux-amd64".to_string(),
        sha256: "a".repeat(64),
        signature: "b".repeat(128),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_update.bin", &encoded);
}

#[test]
fn golden_control_frame_file_list_request() {
    let msg = ControlMessage::FileListRequest {
        path: "/home/ivan".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_list_request.bin", &encoded);
}

#[test]
fn golden_control_frame_file_list_response() {
    let msg = ControlMessage::FileListResponse {
        path: "/home/ivan".to_string(),
        entries: vec![
            FileEntry {
                name: "documents".to_string(),
                is_dir: true,
                size: 0,
                modified: 1_700_000_000,
            },
            FileEntry {
                name: "notes.txt".to_string(),
                is_dir: false,
                size: 2048,
                modified: 1_700_000_100,
            },
        ],
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_list_response.bin", &encoded);
}

#[test]
fn golden_control_frame_file_list_error() {
    let msg = ControlMessage::FileListError {
        path: "/root/secret".to_string(),
        error: "permission denied".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_list_error.bin", &encoded);
}

#[test]
fn golden_control_frame_file_download_request() {
    let msg = ControlMessage::FileDownloadRequest {
        path: "/home/ivan/notes.txt".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_download_request.bin", &encoded);
}

#[test]
fn golden_control_frame_file_upload_request() {
    let msg = ControlMessage::FileUploadRequest {
        path: "/home/ivan/uploads/archive.tar".to_string(),
        total_size: 10_485_760,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_file_upload_request.bin", &encoded);
}

#[test]
fn golden_control_frame_chat_message() {
    let msg = ControlMessage::ChatMessage {
        text: "hello from the operator".to_string(),
        sender: "operator@example.com".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_chat_message.bin", &encoded);
}

// --- Phase A: edge-case goldens ---

#[test]
fn golden_control_agent_register_empty_capabilities() {
    // Optional-field-absent: empty Vec should round-trip as an empty msgpack array,
    // which Go must decode as a nil/empty Capabilities slice.
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![],
        hostname: "headless-ci-runner".to_string(),
        os: "linux".to_string(),
        arch: "aarch64".to_string(),
        version: "0.1.0".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_register_empty_capabilities.bin", &encoded);
}

#[test]
fn golden_control_agent_register_utf8() {
    // Multi-byte UTF-8 (emoji + CJK) must survive round-trip bit-for-bit.
    let msg = ControlMessage::AgentRegister {
        capabilities: vec![AgentCapability::RemoteDesktop],
        hostname: "ラップトップ-🖥️-办公室".to_string(),
        os: "macos".to_string(),
        arch: "aarch64".to_string(),
        version: "0.1.0-αβγ".to_string(),
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    golden_check("control_agent_register_utf8.bin", &encoded);
}

#[test]
fn golden_control_hardware_report_large_size() {
    // Exercise multi-byte length header and heavy payloads without bloating the repo.
    // Each interface record is ~60 bytes; 2000 of them yields a ~100-kiB payload, which
    // forces the 4-byte BE frame length to use its high bytes (0x00 0x01 ...).
    let network_interfaces: Vec<NetworkInterface> = (0..2000)
        .map(|i| NetworkInterface {
            name: format!("veth{i:04}"),
            mac: format!("02:00:00:00:{:02x}:{:02x}", (i >> 8) & 0xff, i & 0xff),
            ipv4: vec![format!("10.0.{}.{}", (i >> 8) & 0xff, i & 0xff)],
            ipv6: vec!["fe80::1".to_string()],
        })
        .collect();
    let msg = ControlMessage::HardwareReport {
        cpu_model: "AMD EPYC 7763 64-Core Processor".to_string(),
        cpu_cores: 128,
        ram_total_mb: 524_288,
        disk_total_mb: 8_388_608,
        disk_free_mb: 4_194_304,
        network_interfaces,
    };
    let frame = Frame::Control(msg);
    let encoded = frame.encode().unwrap();
    assert!(
        encoded.len() > 65_536,
        "large_size golden must exceed 64 KiB to exercise BE length high bytes; got {}",
        encoded.len()
    );
    golden_check("control_hardware_report_large_size.bin", &encoded);
}

#[test]
fn golden_control_chat_message_forward_compat() {
    // Forward-compatibility: an encoder adds a new msgpack key ("future_field")
    // that the current Go decoder does not know about. Go's rmp_serde-compatible
    // msgpack library ignores unknown map keys, so this frame must still decode
    // into a valid ChatMessage with the known fields intact.
    #[derive(Serialize)]
    struct ChatMessageWithExtra<'a> {
        #[serde(rename = "type")]
        ty: &'a str,
        text: &'a str,
        sender: &'a str,
        future_field: &'a str,
        future_number: u32,
    }
    let payload = rmp_serde::to_vec_named(&ChatMessageWithExtra {
        ty: "ChatMessage",
        text: "hello from the future",
        sender: "operator@example.com",
        future_field: "reserved-for-phase-b",
        future_number: 42,
    })
    .unwrap();
    let encoded = frame_wrap(0x01, &payload);
    golden_check("control_chat_message_forward_compat.bin", &encoded);
}

#[test]
fn golden_frame_control_le_length() {
    // Negative test: frame length field encoded little-endian instead of big-endian.
    // Under BE interpretation the declared length (10 in LE = 0x0a000000 in BE =
    // ~167 MiB) exceeds MAX_FRAME_SIZE, so Go's ReadFrame must return
    // ErrFrameTooLarge rather than hang or silently truncate.
    let payload = [0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0x00, 0x00, 0x00, 0x00];
    let mut encoded = Vec::with_capacity(5 + payload.len());
    encoded.push(0x01); // FRAME_CONTROL
    encoded.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    encoded.extend_from_slice(&payload);
    golden_check("frame_control_le_length.bin", &encoded);
}

// --- helpers ---

fn session_token() -> SessionToken {
    // SessionToken has no public constructor-from-string; we generate a token and
    // then override its contents via msgpack round-trip to keep determinism.
    let fixed: SessionTokenShim = SessionTokenShim(GOLDEN_SESSION_TOKEN.to_string());
    let bytes = rmp_serde::to_vec(&fixed).unwrap();
    rmp_serde::from_slice(&bytes).unwrap()
}

#[derive(Serialize)]
struct SessionTokenShim(String);

fn frame_wrap(frame_type: u8, payload: &[u8]) -> Vec<u8> {
    let mut buf = Vec::with_capacity(5 + payload.len());
    buf.push(frame_type);
    buf.extend_from_slice(&(payload.len() as u32).to_be_bytes());
    buf.extend_from_slice(payload);
    buf
}
