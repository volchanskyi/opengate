//! Substrate B+ — the [`compact`](crate::compact) codec packed into **big
//! blocks** inside redb.
//!
//! Two changes from [`RedbStore`](crate::redb_store), isolating the two Path-1
//! levers:
//!
//! - **compact blocks** (float32 + implicit-ts + adaptive codec) instead of
//!   lossless f64 Gorilla — the encoding-density lever;
//! - a **large seal size** so one redb key maps to a fat multi-thousand-sample
//!   value — Netdata's extent idea, approximated so redb's per-entry B-tree
//!   overhead amortises toward zero (the write-amp lever the small-block f64
//!   store paid ~2.8× for).
//!
//! It inherits redb's COW crash safety, MVCC reads, and integrity check
//! unchanged — the substrate ideas that are redb's, not ours to rebuild.

use std::collections::BTreeMap;
use std::path::Path;

use crate::compact::{decode_compact, encode_compact};
use crate::error::Result;
use crate::redb_backend::RedbBackend;
use crate::sample::{Sample, SeriesId};
use crate::substrate::{Durability, Substrate};

/// Samples per sealed block. Large on purpose: at ~1 B/sample a 3000-sample
/// block is ~3 KB, so redb's fixed per-key overhead is a rounding error.
const BIG_CHUNK_SAMPLES: usize = 3_000;
/// Fixed sampler cadence assumed by the implicit-timestamp codec.
const STEP_SECS: i64 = 1;

/// Substrate B+. The compact codec over the shared [`RedbBackend`].
pub struct RedbCompactStore {
    backend: RedbBackend,
    open_chunks: BTreeMap<SeriesId, Vec<Sample>>,
}

impl RedbCompactStore {
    fn seal(&mut self, series: SeriesId) {
        if let Some(samples) = self.open_chunks.remove(&series) {
            if let Some(first) = samples.first() {
                let first_ts = first.ts;
                let anomaly = vec![false; samples.len()];
                let block = encode_compact(&samples, &anomaly, STEP_SECS);
                self.backend.pending.push((series, first_ts, block));
            }
        }
    }
}

impl Substrate for RedbCompactStore {
    fn open(path: &Path) -> Result<Self> {
        Ok(Self {
            backend: RedbBackend::open(path, "compact.redb", "compact_chunks")?,
            open_chunks: BTreeMap::new(),
        })
    }

    fn append(&mut self, series: SeriesId, sample: Sample) -> Result<()> {
        let buf = self.open_chunks.entry(series).or_default();
        buf.push(sample);
        if buf.len() >= BIG_CHUNK_SAMPLES {
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
        let mut err = None;
        self.backend
            .for_each_block(series, |block| match decode_compact(block) {
                Ok((samples, _bits)) => {
                    out.extend(samples.into_iter().filter(|s| s.ts >= start && s.ts < end));
                }
                Err(e) => err = Some(e),
            })?;
        if let Some(e) = err {
            return Err(e);
        }
        for (_s, _ts, block) in self.backend.pending.iter().filter(|(s, _, _)| *s == series) {
            let (samples, _bits) = decode_compact(block)?;
            out.extend(samples.into_iter().filter(|s| s.ts >= start && s.ts < end));
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
            .total_samples(|b| crate::compact::block_count(b) as usize, open)
    }
}

#[cfg(test)]
mod tests {
    use super::RedbCompactStore;
    use crate::sample::Sample;
    use crate::substrate::{Durability, Substrate};

    #[test]
    fn persists_reopens_within_tolerance() {
        let dir = tempfile::tempdir().unwrap();
        let n = 5_000i64; // spans multiple big blocks
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

    #[test]
    fn empty_store_reads_clean() {
        let dir = tempfile::tempdir().unwrap();
        let s = RedbCompactStore::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), 0);
        assert!(s.range(1, 0, 100).unwrap().is_empty());
    }
}
