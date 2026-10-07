//! Substrate B: Gorilla blocks of two minutes at 1 Hz over the shared [`ChunkedStore`].

use crate::error::Result;
use crate::gorilla::{block_count, decode_block, encode_block};
use crate::redb_backend::{BlockCodec, ChunkedStore};
use crate::sample::Sample;

/// The Gorilla codec, sealing at the cadence substrate A seals at.
pub struct GorillaCodec;

impl BlockCodec for GorillaCodec {
    const FILE: &'static str = "store.redb";
    const TABLE: &'static str = "chunks";
    const CHUNK_SAMPLES: usize = 120;
    const STRICT: bool = false;

    fn encode(samples: &[Sample]) -> Vec<u8> {
        encode_block(samples)
    }

    fn decode(block: &[u8]) -> Result<Vec<Sample>> {
        decode_block(block)
    }

    fn count(block: &[u8]) -> usize {
        block_count(block) as usize
    }
}

/// Substrate B. The Gorilla codec over the shared [`ChunkedStore`].
pub type RedbStore = ChunkedStore<GorillaCodec>;

#[cfg(test)]
mod tests {
    use super::RedbStore;
    use crate::sample::Sample;
    use crate::substrate::{Durability, Substrate};

    #[test]
    fn persists_and_reopens() {
        let dir = tempfile::tempdir().unwrap();
        {
            let mut s = RedbStore::open(dir.path()).unwrap();
            for i in 0..400 {
                s.append(1, Sample::new(1_000 + i, (i % 5) as f64)).unwrap();
            }
            s.commit(Durability::Full).unwrap();
            assert_eq!(s.total_samples().unwrap(), 400);
        }
        let s = RedbStore::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), 400);
        assert_eq!(s.range(1, 1_000, 2_000).unwrap().len(), 400);
        assert!(s.size_on_disk().unwrap() > 0);
    }
}
