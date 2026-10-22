//! The connection close codes match the table the Go server is held to.

use std::collections::BTreeMap;
use std::path::PathBuf;

#[test]
fn close_codes_match_the_shared_table() {
    let path = std::env::var("OPENGATE_GOLDEN_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|_| {
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../testdata/golden")
        })
        .join("close_codes.json");
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
    let table: BTreeMap<String, u32> = serde_json::from_str(&text).expect("parse close codes");

    use mesh_protocol::close_code::*;
    let ours: BTreeMap<String, u32> = [
        ("bye", BYE),
        ("stream_accept_failed", STREAM_ACCEPT_FAILED),
        ("handshake_refused", HANDSHAKE_REFUSED),
        ("device_deleted", DEVICE_DELETED),
        ("restricted_session_ended", RESTRICTED_SESSION_ENDED),
        ("certificate_refused", CERTIFICATE_REFUSED),
        ("replaced_by_newer_connection", REPLACED_BY_NEWER_CONNECTION),
    ]
    .into_iter()
    .map(|(k, v)| (k.to_string(), v))
    .collect();
    assert_eq!(table, ours);
}
