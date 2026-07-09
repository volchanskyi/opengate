//! Stable-toolchain regression guard for the `decode` libFuzzer target.
//!
//! `agent/fuzz/fuzz_targets/decode.rs` fuzzes [`Frame::decode`] under a nightly
//! toolchain (libFuzzer needs nightly). This test is the always-run counterpart:
//! it replays every committed corpus seed and every minimized crash fixture
//! through `Frame::decode` on **stable**, asserting the decoder never panics on
//! arbitrary/adversarial bytes — the agent's network attack surface.
//!
//! Per tests-determinism.md it runs unconditionally under `cargo test` with no
//! skip/build-tag gating: the committed fuzz corpus (crafted edge cases covering
//! every decode branch, plus a real encoded frame and any minimized crashes) is
//! always present in the source tree, so the replay always does real work.

use mesh_protocol::Frame;
use std::path::PathBuf;

/// The committed libFuzzer corpus for the `decode` target — crafted edge cases
/// plus any minimized crash inputs. Always present in the source tree.
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

        // A typed Err is fine; a panic (unchecked index, slice overflow, …) is
        // not — that is exactly the class of bug the fuzz target hunts for.
        // `is_err()` consumes the `#[must_use]` result while keeping the point
        // of the replay explicit: reaching this line means decode returned
        // instead of panicking.
        let _decoded_without_panic = Frame::decode(&bytes).is_err();
        // Decoding any prefix of a seed must also stay panic-free (libFuzzer
        // routinely feeds truncated inputs as it minimizes).
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
