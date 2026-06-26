#![no_main]

use libfuzzer_sys::fuzz_target;
use mesh_protocol::Frame;

// Feed arbitrary bytes straight into the wire decoder. `Frame::decode` takes
// `&[u8]`, so no `Arbitrary` impl is needed. The invariant: decoding any input
// returns a typed Err at worst, never panics or triggers UB. The committed
// corpus under corpus/decode seeds the run; the always-run stable guard is
// crates/mesh-protocol/tests/decode_corpus_test.rs.
fuzz_target!(|data: &[u8]| {
    let _ = Frame::decode(data);
});
