//! Server → agent control messages omit zero-valued fields on the wire (`omitempty`); a decode
//! error breaks the control loop, so legal zero values decode from a bare `{"type": X}` map.

use mesh_protocol::{ControlMessage, Permissions, SessionToken};
use std::collections::BTreeMap;

fn encode_map(pairs: &[(&str, &str)]) -> Vec<u8> {
    let map: BTreeMap<&str, &str> = pairs.iter().copied().collect();
    rmp_serde::to_vec_named(&map).expect("encode probe map")
}

fn decode(pairs: &[(&str, &str)]) -> Result<ControlMessage, rmp_serde::decode::Error> {
    rmp_serde::from_slice(&encode_map(pairs))
}

fn decode_ok(pairs: &[(&str, &str)]) -> ControlMessage {
    decode(pairs).unwrap_or_else(|e| panic!("expected {pairs:?} to decode, got {e:?}"))
}

fn decode_err(pairs: &[(&str, &str)]) -> String {
    match decode(pairs) {
        Ok(msg) => panic!("expected {pairs:?} to fail decoding, got {msg:?}"),
        Err(e) => e.to_string(),
    }
}

#[test]
fn restart_agent_decodes_without_reason() {
    match decode_ok(&[("type", "RestartAgent")]) {
        ControlMessage::RestartAgent { reason } => assert_eq!(reason, ""),
        other => panic!("expected RestartAgent, got {other:?}"),
    }
}

#[test]
fn restart_agent_keeps_a_present_reason() {
    match decode_ok(&[("type", "RestartAgent"), ("reason", "operator request")]) {
        ControlMessage::RestartAgent { reason } => assert_eq!(reason, "operator request"),
        other => panic!("expected RestartAgent, got {other:?}"),
    }
}

#[test]
fn agent_deregistered_decodes_without_reason() {
    match decode_ok(&[("type", "AgentDeregistered")]) {
        ControlMessage::AgentDeregistered { reason } => assert_eq!(reason, ""),
        other => panic!("expected AgentDeregistered, got {other:?}"),
    }
}

#[test]
fn agent_deregistered_keeps_a_present_reason() {
    match decode_ok(&[("type", "AgentDeregistered"), ("reason", "device deleted")]) {
        ControlMessage::AgentDeregistered { reason } => assert_eq!(reason, "device deleted"),
        other => panic!("expected AgentDeregistered, got {other:?}"),
    }
}

#[test]
fn request_hardware_report_decodes_as_a_bare_unit_variant() {
    assert!(matches!(
        decode_ok(&[("type", "RequestHardwareReport")]),
        ControlMessage::RequestHardwareReport
    ));
}

#[test]
fn request_device_logs_decodes_with_every_field_defaulted() {
    match decode_ok(&[("type", "RequestDeviceLogs")]) {
        ControlMessage::RequestDeviceLogs {
            log_level,
            time_from,
            time_to,
            search,
            log_offset,
            log_limit,
            source,
            unit,
        } => {
            assert_eq!(log_level, "");
            assert_eq!(time_from, "");
            assert_eq!(time_to, "");
            assert_eq!(search, "");
            assert_eq!(log_offset, 0);
            assert_eq!(log_limit, 0);
            assert_eq!(source, "");
            assert_eq!(unit, "");
        }
        other => panic!("expected RequestDeviceLogs, got {other:?}"),
    }
}

#[test]
fn session_request_fails_closed_without_a_token() {
    let err = decode_err(&[("type", "SessionRequest")]);
    assert!(
        err.contains("missing field") && err.contains("token"),
        "error should name the missing token field, got: {err}"
    );
}

#[test]
fn session_request_fails_closed_without_a_relay_url() {
    let err = decode_err(&[("type", "SessionRequest"), ("token", "deadbeef")]);
    assert!(
        err.contains("missing field") && err.contains("relay_url"),
        "error should name the missing relay_url field, got: {err}"
    );
}

#[test]
fn session_request_decodes_when_fully_populated() {
    // `permissions` is a struct, so the probe is built from the typed value.
    let msg = ControlMessage::SessionRequest {
        token: SessionToken::generate(),
        relay_url: "wss://relay.example.com/relay".to_string(),
        permissions: Permissions {
            desktop: true,
            terminal: true,
            file_read: false,
            file_write: false,
            input: true,
        },
    };
    let bytes = rmp_serde::to_vec_named(&msg).expect("encode SessionRequest");
    let decoded: ControlMessage = rmp_serde::from_slice(&bytes).expect("decode SessionRequest");
    assert_eq!(decoded, msg);
}

#[test]
fn agent_update_fails_closed_without_a_version() {
    let err = decode_err(&[("type", "AgentUpdate")]);
    assert!(
        err.contains("missing field") && err.contains("version"),
        "error should name the missing version field, got: {err}"
    );
}

#[test]
fn agent_update_fails_closed_without_a_signature() {
    let err = decode_err(&[
        ("type", "AgentUpdate"),
        ("version", "0.15.4"),
        ("url", "https://example.com/agent"),
    ]);
    assert!(
        err.contains("missing field") && err.contains("signature"),
        "error should name the missing signature field, got: {err}"
    );
}

#[test]
fn agent_update_decodes_with_an_absent_sha256() {
    // An absent sha256 skips the hash comparison; the signature is still verified over the
    // downloaded hash.
    match decode_ok(&[
        ("type", "AgentUpdate"),
        ("version", "0.15.4"),
        ("url", "https://example.com/agent"),
        ("signature", "ed25519-signature"),
    ]) {
        ControlMessage::AgentUpdate {
            version,
            url,
            sha256,
            signature,
        } => {
            assert_eq!(version, "0.15.4");
            assert_eq!(url, "https://example.com/agent");
            assert_eq!(sha256, "");
            assert_eq!(signature, "ed25519-signature");
        }
        other => panic!("expected AgentUpdate, got {other:?}"),
    }
}
