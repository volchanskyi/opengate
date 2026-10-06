//! Cold-tier DEFLATE through pure-Rust `flate2`, applied to sealed T1/T2 rollup blocks only
//! ([`LocalTsdb::compact_cold_tiers`](crate::store::LocalTsdb::compact_cold_tiers)).

use std::io::{Read, Write};

use flate2::read::DeflateDecoder;
use flate2::write::DeflateEncoder;
use flate2::Compression;

use crate::error::{Result, TsdbError};

/// DEFLATE-compresses a block, surfacing any write error as [`TsdbError::Io`].
pub fn deflate(bytes: &[u8]) -> Result<Vec<u8>> {
    let mut enc = DeflateEncoder::new(Vec::new(), Compression::default());
    enc.write_all(bytes)?;
    Ok(enc.finish()?)
}

/// Inflates a block produced by [`deflate`]; a truncated or corrupt stream is a
/// [`TsdbError::CorruptBlock`].
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
