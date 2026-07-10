//! Optional cold-tier DEFLATE — pure-Rust `flate2`/`miniz_oxide` (no CGO, no new
//! crates: already in the agent lock).
//!
//! DEFLATE is applied **only** to sealed T1/T2 rollup blocks
//! ([`LocalTsdb::compact_cold_tiers`](crate::store::LocalTsdb::compact_cold_tiers)),
//! never to hot T0 raw — it buys density on the cold tiers at the cost of
//! decompress CPU on read, which is why it is opt-in and gated on the agent's
//! <1 % CPU budget. `zstd` is deliberately not used: the WS-14a measurement
//! showed a C `zstd` dependency buys no more than pure-Rust DEFLATE over the
//! bit-packed codec, so it is unjustified.

use std::io::{Read, Write};

use flate2::read::DeflateDecoder;
use flate2::write::DeflateEncoder;
use flate2::Compression;

use crate::error::{Result, TsdbError};

/// DEFLATE-compress a block. Writing to a `Vec` cannot fail in practice; any
/// error is surfaced as [`TsdbError::Io`] rather than panicking.
pub fn deflate(bytes: &[u8]) -> Result<Vec<u8>> {
    let mut enc = DeflateEncoder::new(Vec::new(), Compression::default());
    enc.write_all(bytes)?;
    Ok(enc.finish()?)
}

/// Inflate a block produced by [`deflate`]. A truncated or corrupt stream is a
/// [`TsdbError::CorruptBlock`], never a panic.
pub fn inflate(bytes: &[u8]) -> Result<Vec<u8>> {
    let mut out = Vec::new();
    DeflateDecoder::new(bytes)
        .read_to_end(&mut out)
        .map_err(|_| TsdbError::CorruptBlock("deflate"))?;
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::{deflate, inflate};

    #[test]
    fn round_trips_and_shrinks_repetitive_data() {
        // A struct-of-arrays tier block is highly repetitive — DEFLATE must
        // round-trip it and materially shrink it.
        let raw: Vec<u8> = (0..4000u32).flat_map(|i| (i / 40).to_le_bytes()).collect();
        let z = deflate(&raw).unwrap();
        assert!(
            z.len() < raw.len() / 2,
            "deflate did not shrink: {} -> {}",
            raw.len(),
            z.len()
        );
        assert_eq!(inflate(&z).unwrap(), raw);
    }

    #[test]
    fn inflate_rejects_garbage_without_panic() {
        assert!(inflate(&[0xFF, 0x00, 0x13, 0x37, 0x42]).is_err());
    }
}
