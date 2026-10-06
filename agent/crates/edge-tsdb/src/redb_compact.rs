//! Substrate B+: the [`compact`](crate::compact) codec packed into multi-thousand-sample redb
//! values, which amortises redb's per-key B-tree overhead.

use crate::compact::{block_count, decode_compact, encode_compact};
use crate::error::Result;
use crate::redb_backend::{BlockCodec, ChunkedStore};
use crate::sample::Sample;

/// Fixed sampler cadence in seconds assumed by the implicit-timestamp codec.
const STEP_SECS: i64 = 1;

/// The compact codec with no anomaly bits; at about 1 byte per sample a block is about 3 KB.
pub struct CompactCodec;

impl BlockCodec for CompactCodec {
    const FILE: &'static str = "compact.redb";
    const TABLE: &'static str = "compact_chunks";
    const CHUNK_SAMPLES: usize = 3_000;
    const STRICT: bool = true;

    fn encode(samples: &[Sample]) -> Vec<u8> {
        encode_compact(samples, &vec![false; samples.len()], STEP_SECS)
    }

    fn decode(block: &[u8]) -> Result<Vec<Sample>> {
        decode_compact(block).map(|(samples, _bits)| samples)
    }

    fn count(block: &[u8]) -> usize {
        block_count(block) as usize
    }
}

/// Substrate B+. The compact codec over the shared [`ChunkedStore`].
pub type RedbCompactStore = ChunkedStore<CompactCodec>;

#[cfg(test)]
mod tests {
    use super::RedbCompactStore;
    use crate::sample::Sample;
    use crate::substrate::{Durability, Substrate};

    #[test]
    fn persists_reopens_within_tolerance() {
        let dir = tempfile::tempdir().unwrap();
        let n = 5_000i64;
        {
            let mut s = RedbCompactStore::open(dir.path()).unwrap();
            for i in 0..n {
                s.append(1, Sample::new(1_000 + i, 40.0 + (i % 8) as f64 * 0.1))
                    .unwrap();
            }
            s.commit(Durability::Full).unwrap();
            assert_eq!(s.total_samples().unwrap(), n as usize);
        }
        let s = RedbCompactStore::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), n as usize);
        let got = s.range(1, i64::MIN, i64::MAX).unwrap();
        assert_eq!(got.len(), n as usize);
        for (i, sample) in got.iter().enumerate() {
            let want = 40.0 + (i as i64 % 8) as f64 * 0.1;
            assert!((sample.value - want).abs() < 1e-4);
        }
        assert!(s.size_on_disk().unwrap() > 0);
    }
}
