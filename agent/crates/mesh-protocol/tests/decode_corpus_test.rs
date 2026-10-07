//! Replays the committed `decode` fuzz corpus through [`Frame::decode`] on stable, asserting it
//! never panics on adversarial bytes.

use mesh_protocol::Frame;
use std::path::PathBuf;

fn corpus_dir() -> PathBuf {
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("../../fuzz/corpus/decode");
    path
}

#[test]
fn decode_never_panics_on_committed_corpus() {
    let dir = corpus_dir();
    let entries = std::fs::read_dir(&dir)
        .unwrap_or_else(|e| panic!("read corpus dir {}: {e}", dir.display()));

    let mut count = 0;
    for entry in entries {
        let path = entry.expect("corpus dir entry").path();
        if !path.is_file() {
            continue;
        }
        let bytes = std::fs::read(&path)
            .unwrap_or_else(|e| panic!("read corpus file {}: {e}", path.display()));
        count += 1;

        // `is_err()` consumes the `#[must_use]` result; reaching it means decode returned.
        let _decoded_without_panic = Frame::decode(&bytes).is_err();
        // Truncated prefixes of a seed must stay panic-free as well.
        for cut in [0usize, 1, bytes.len() / 2] {
            if cut <= bytes.len() {
                let _decoded_without_panic = Frame::decode(&bytes[..cut]).is_err();
            }
        }
    }

    assert!(
        count > 0,
        "decode corpus {} is empty — seeds must be committed",
        dir.display()
    );
}
