//! The request error texts match the table the Go server is held to.

use std::collections::BTreeMap;
use std::path::PathBuf;

#[test]
fn request_errors_match_the_shared_table() {
    let path = std::env::var("OPENGATE_GOLDEN_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|_| {
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../testdata/golden")
        })
        .join("request_errors.json");
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
    let table: BTreeMap<String, String> =
        serde_json::from_str(&text).expect("parse request errors");

    use mesh_protocol::request_error::*;
    let ours: BTreeMap<String, String> = [("busy", BUSY), ("timed_out", TIMED_OUT)]
        .into_iter()
        .map(|(k, v)| (k.to_string(), v.to_string()))
        .collect();
    assert_eq!(table, ours);
}
