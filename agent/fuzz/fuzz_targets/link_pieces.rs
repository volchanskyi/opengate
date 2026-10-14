#![no_main]

use libfuzzer_sys::fuzz_target;
use mesh_protocol::direct::{CandidateFilter, Reassembler, MAX_LINK_FRAME};

// Hostile pieces and candidate lines cost at most one link frame of memory and never panic.
fuzz_target!(|data: &[u8]| {
    let mut reassembler = Reassembler::new();
    for piece in data.split(|b| *b == 0xff) {
        if reassembler.push(piece).is_err() {
            reassembler.reset();
        }
        assert!(reassembler.pending() <= MAX_LINK_FRAME + piece.len());
    }

    let text = String::from_utf8_lossy(data);
    let mut filter = CandidateFilter::new();
    for line in text.lines() {
        let _ = filter.admit(line);
    }
    let _ = filter.filter_sdp(&text);
});
