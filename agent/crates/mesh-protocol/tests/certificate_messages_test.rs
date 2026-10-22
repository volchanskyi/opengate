//! The six certificate messages in both directions: Rust-encoded goldens the Go decoder reads,
//! Go-encoded goldens this decoder reads, and the bare shapes the Go encoder sends.

use mesh_protocol::*;
use std::collections::BTreeMap;
use std::path::PathBuf;

const GOLDEN_CSR: &[u8] = &[0x30, 0x81, 0x9a, 0x02, 0x01, 0x00, 0xc5, 0x9e];
const GOLDEN_CERT: &[u8] = &[0x30, 0x82, 0x01, 0x4b, 0x30, 0x81, 0xf1, 0xa0, 0x03];
const GOLDEN_BUNDLE: &str =
    "-----BEGIN CERTIFICATE-----\nZ29sZGVuLWJ1bmRsZQ==\n-----END CERTIFICATE-----\n";

fn golden_dir() -> PathBuf {
    if let Ok(dir) = std::env::var("OPENGATE_GOLDEN_DIR") {
        return PathBuf::from(dir);
    }
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../testdata/golden")
}

fn golden_check(name: &str, data: &[u8]) {
    let path = golden_dir().join(name);
    if std::env::var("GENERATE_GOLDEN").is_ok_and(|v| v == "1") {
        std::fs::write(&path, data).unwrap();
        return;
    }
    let expected = std::fs::read(&path).unwrap_or_else(|e| {
        panic!(
            "golden {} missing, run GENERATE_GOLDEN=1: {e}",
            path.display()
        )
    });
    assert_eq!(data, &expected[..], "golden mismatch: {}", path.display());
}

fn decode_go_golden(name: &str) -> ControlMessage {
    let path = golden_dir().join(name);
    let bytes = std::fs::read(&path).unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
    let (frame, consumed) =
        Frame::decode(&bytes).unwrap_or_else(|e| panic!("decoder rejected {name}: {e:?}"));
    assert_eq!(consumed, bytes.len(), "{name} was not consumed whole");
    match frame {
        Frame::Control(msg) => msg,
        other => panic!("{name}: expected a control frame, got {other:?}"),
    }
}

fn decode_map(pairs: &[(&str, &str)]) -> ControlMessage {
    let map: BTreeMap<&str, &str> = pairs.iter().copied().collect();
    let bytes = rmp_serde::to_vec_named(&map).expect("encode probe map");
    rmp_serde::from_slice(&bytes).unwrap_or_else(|e| panic!("{pairs:?} must decode: {e}"))
}

#[test]
fn certificate_signing_request_golden() {
    let frame = Frame::Control(ControlMessage::CertificateSigningRequest {
        csr: GOLDEN_CSR.to_vec(),
    });
    golden_check(
        "control_certificate_signing_request.bin",
        &frame.encode().unwrap(),
    );
}

#[test]
fn certificate_installed_golden() {
    let frame = Frame::Control(ControlMessage::CertificateInstalled);
    golden_check(
        "control_certificate_installed.bin",
        &frame.encode().unwrap(),
    );
}

#[test]
fn go_renew_certificate_decodes() {
    match decode_go_golden("go_control_renew_certificate.bin") {
        ControlMessage::RenewCertificate { reason } => assert_eq!(reason, "authority-change"),
        other => panic!("expected RenewCertificate, got {other:?}"),
    }
    match decode_go_golden("go_control_renew_certificate_min.bin") {
        ControlMessage::RenewCertificate { reason } => assert_eq!(reason, ""),
        other => panic!("expected RenewCertificate, got {other:?}"),
    }
}

#[test]
fn go_certificate_issued_decodes() {
    match decode_go_golden("go_control_certificate_issued.bin") {
        ControlMessage::CertificateIssued { cert, ca_bundle } => {
            assert_eq!(cert, GOLDEN_CERT);
            assert_eq!(ca_bundle, GOLDEN_BUNDLE);
        }
        other => panic!("expected CertificateIssued, got {other:?}"),
    }
}

#[test]
fn go_certificate_refused_decodes() {
    match decode_go_golden("go_control_certificate_refused.bin") {
        ControlMessage::CertificateRefused {
            reason,
            retry_after,
        } => {
            assert_eq!(reason, "rate-limited");
            assert_eq!(retry_after, 3600);
        }
        other => panic!("expected CertificateRefused, got {other:?}"),
    }
    match decode_go_golden("go_control_certificate_refused_min.bin") {
        ControlMessage::CertificateRefused {
            reason,
            retry_after,
        } => {
            assert_eq!(reason, "");
            assert_eq!(retry_after, 0);
        }
        other => panic!("expected CertificateRefused, got {other:?}"),
    }
}

#[test]
fn go_trust_bundle_decodes() {
    match decode_go_golden("go_control_trust_bundle.bin") {
        ControlMessage::TrustBundle { ca_bundle } => assert_eq!(ca_bundle, GOLDEN_BUNDLE),
        other => panic!("expected TrustBundle, got {other:?}"),
    }
}

#[test]
fn server_to_agent_certificate_messages_decode_from_a_bare_type() {
    assert!(matches!(
        decode_map(&[("type", "RenewCertificate")]),
        ControlMessage::RenewCertificate { reason } if reason.is_empty()
    ));
    assert!(matches!(
        decode_map(&[("type", "CertificateIssued")]),
        ControlMessage::CertificateIssued { cert, ca_bundle } if cert.is_empty() && ca_bundle.is_empty()
    ));
    assert!(matches!(
        decode_map(&[("type", "CertificateRefused")]),
        ControlMessage::CertificateRefused { reason, retry_after: 0 } if reason.is_empty()
    ));
    assert!(matches!(
        decode_map(&[("type", "TrustBundle")]),
        ControlMessage::TrustBundle { ca_bundle } if ca_bundle.is_empty()
    ));
}
