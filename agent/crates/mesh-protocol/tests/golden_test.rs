//! Golden file tests for cross-language protocol compatibility.
//!
//! When GENERATE_GOLDEN=1, generates golden files to testdata/golden/.
//! Otherwise, reads and verifies existing golden files.

use mesh_protocol::*;
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
fn golden_handshake_server_hello() {
    let msg = HandshakeMessage::ServerHello {
        nonce: [0xAA; 32],
        cert_hash: [0xBB; 48],
    };
    let encoded = msg.encode_binary();
    golden_check("handshake_server_hello.bin", &encoded);
}
