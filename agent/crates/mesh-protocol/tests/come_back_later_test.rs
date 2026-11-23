//! The server's "come back later" answer to a sign-in in both directions: the Rust-encoded golden
//! the Go decoder reads, the Go-encoded goldens this decoder reads, and the bare shape.

use mesh_protocol::*;
use std::collections::BTreeMap;
use std::path::PathBuf;

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

#[test]
fn come_back_later_golden() {
    let frame = Frame::Control(ControlMessage::ComeBackLater { retry_after: 17 });
    golden_check("control_come_back_later.bin", &frame.encode().unwrap());
}

#[test]
fn go_come_back_later_decodes_with_its_wait() {
    match decode_go_golden("go_control_come_back_later.bin") {
        ControlMessage::ComeBackLater { retry_after } => assert_eq!(retry_after, 17),
        other => panic!("expected ComeBackLater, got {other:?}"),
    }
}

#[test]
fn go_come_back_later_with_no_wait_decodes_as_at_once() {
    match decode_go_golden("go_control_come_back_later_min.bin") {
        ControlMessage::ComeBackLater { retry_after } => assert_eq!(retry_after, 0),
        other => panic!("expected ComeBackLater, got {other:?}"),
    }
}

#[test]
fn a_bare_come_back_later_decodes() {
    let map: BTreeMap<&str, &str> = [("type", "ComeBackLater")].into_iter().collect();
    let bytes = rmp_serde::to_vec_named(&map).expect("encode probe map");
    let msg: ControlMessage = rmp_serde::from_slice(&bytes).expect("a bare shape decodes");
    assert_eq!(msg, ControlMessage::ComeBackLater { retry_after: 0 });
}
