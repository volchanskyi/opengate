//! Replays the committed `link_pieces` fuzz corpus on stable: hostile pieces and candidate lines
//! never panic, and the reassembler never holds more than one link frame.

use mesh_protocol::direct::{CandidateFilter, Reassembler, MAX_LINK_FRAME};
use std::path::PathBuf;

fn corpus_dir() -> PathBuf {
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("../../fuzz/corpus/link_pieces");
    path
}

fn replay(data: &[u8]) {
    let mut reassembler = Reassembler::new();
    for piece in data.split(|b| *b == 0xff) {
        if reassembler.push(piece).is_err() {
            reassembler.reset();
        }
        assert!(reassembler.pending() <= MAX_LINK_FRAME + piece.len());
    }
    let text = String::from_utf8_lossy(data);
    let mut filter = CandidateFilter::new();
    let admitted = text.lines().filter(|line| filter.admit(line)).count();
    assert!(admitted <= 32);
    let _kept = filter.filter_sdp(&text);
}

#[test]
fn link_pieces_never_panic_on_the_committed_corpus() {
    let dir = corpus_dir();
    let mut count = 0;
    for entry in std::fs::read_dir(&dir).unwrap_or_else(|e| panic!("read {}: {e}", dir.display())) {
        let path = entry.expect("corpus entry").path();
        if !path.is_file() {
            continue;
        }
        let bytes = std::fs::read(&path).unwrap();
        replay(&bytes);
        for cut in [0, 1, bytes.len() / 2] {
            if cut <= bytes.len() {
                replay(&bytes[..cut]);
            }
        }
        count += 1;
    }
    assert!(count > 0, "link_pieces corpus {} is empty", dir.display());
}

#[test]
fn a_flood_of_partial_frames_holds_one_frame_at_most() {
    let mut header = vec![0x04];
    header.extend_from_slice(&((MAX_LINK_FRAME - 5) as u32).to_be_bytes());
    let mut reassembler = Reassembler::new();
    assert!(reassembler.push(&header).unwrap().is_empty());
    let mut remaining = MAX_LINK_FRAME - header.len();
    let mut completed = Vec::new();
    while remaining > 0 {
        let piece = remaining.min(16 * 1024);
        completed.extend(reassembler.push(&vec![0u8; piece]).unwrap());
        remaining -= piece;
        assert!(reassembler.pending() <= MAX_LINK_FRAME);
    }
    assert_eq!(
        completed.len(),
        1,
        "the largest frame completes with its last piece"
    );
    assert_eq!(completed[0].len(), MAX_LINK_FRAME);
    assert_eq!(reassembler.pending(), 0);
}
