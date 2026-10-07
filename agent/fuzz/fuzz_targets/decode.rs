#![no_main]

use libfuzzer_sys::fuzz_target;
use mesh_protocol::Frame;

// Decoding any byte input returns a typed Err at worst, without a panic or UB.
fuzz_target!(|data: &[u8]| {
    let _ = Frame::decode(data);
});
