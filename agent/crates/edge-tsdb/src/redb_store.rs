//! Substrate B — the shared Gorilla blocks persisted in a redb table.
//!
//! redb is a pure-Rust, zero-dependency, MIT/Apache COW B-tree with a two-phase
//! commit and a built-in integrity check. It contributes the crash-safety code
//! we would otherwise own in substrate A, at the cost of B-tree storage overhead
//! per chunk. Chunks seal in memory and are written in one transaction per
//! commit; `Durability::Full` maps to redb's `Immediate` (fsync) commit and
//! `None` to its buffered commit.

use std::collections::BTreeMap;
use std::path::Path;

use crate::error::Result;
use crate::gorilla::encode_block;
use crate::redb_backend::RedbBackend;
use crate::sample::{Sample, SeriesId};
use crate::substrate::{Durability, Substrate};

/// Samples per sealed chunk (2 minutes at 1 Hz) — matches substrate A so the
/// comparison isolates storage overhead.
const CHUNK_SAMPLES: usize = 120;

/// Substrate B. The Gorilla codec over the shared [`RedbBackend`].
pub struct RedbStore {
    backend: RedbBackend,
    open_chunks: BTreeMap<SeriesId, Vec<Sample>>,
}

impl RedbStore {
    fn seal(&mut self, series: SeriesId) {
        if let Some(samples) = self.open_chunks.remove(&series) {
            if let Some(first) = samples.first() {
                let first_ts = first.ts;
                self.backend
                    .pending
                    .push((series, first_ts, encode_block(&samples)));
            }
        }
    }
}

impl Substrate for RedbStore {
    fn open(path: &Path) -> Result<Self> {
        Ok(Self {
            backend: RedbBackend::open(path, "store.redb", "chunks")?,
            open_chunks: BTreeMap::new(),
        })
    }

    fn append(&mut self, series: SeriesId, sample: Sample) -> Result<()> {
        let buf = self.open_chunks.entry(series).or_default();
        buf.push(sample);
        if buf.len() >= CHUNK_SAMPLES {
            self.seal(series);
        }
        Ok(())
    }

    fn commit(&mut self, durability: Durability) -> Result<()> {
        let series: Vec<SeriesId> = self.open_chunks.keys().copied().collect();
        for s in series {
            self.seal(s);
        }
        self.backend.write_pending(durability)
    }

    fn range(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<Sample>> {
        let mut out = Vec::new();
        self.backend.for_each_block(series, |block| {
            if let Ok(samples) = crate::gorilla::decode_block(block) {
                out.extend(samples.into_iter().filter(|s| s.ts >= start && s.ts < end));
            }
        })?;
        for (s, _ts, block) in self.backend.pending.iter().filter(|(s, _, _)| *s == series) {
            debug_assert_eq!(*s, series);
            out.extend(
                crate::gorilla::decode_block(block)?
                    .into_iter()
                    .filter(|s| s.ts >= start && s.ts < end),
            );
        }
        if let Some(buf) = self.open_chunks.get(&series) {
            out.extend(buf.iter().filter(|s| s.ts >= start && s.ts < end).copied());
        }
        out.sort_by_key(|s| s.ts);
        Ok(out)
    }

    fn size_on_disk(&self) -> Result<u64> {
        self.backend.size_on_disk()
    }

    fn total_samples(&self) -> Result<usize> {
        let open = self.open_chunks.values().map(Vec::len).sum::<usize>();
        self.backend
            .total_samples(|b| crate::gorilla::block_count(b) as usize, open)
    }
}

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

    #[test]
    fn empty_store_reads_clean() {
        let dir = tempfile::tempdir().unwrap();
        let s = RedbStore::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), 0);
        assert!(s.range(1, 0, 100).unwrap().is_empty());
    }
}
