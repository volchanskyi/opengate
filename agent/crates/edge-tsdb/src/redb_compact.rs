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
use std::path::{Path, PathBuf};

use redb::{Database, ReadableDatabase, ReadableTable, TableDefinition};

use crate::compact::{decode_compact, encode_compact};
use crate::error::{Result, TsdbError};
use crate::sample::{Sample, SeriesId};
use crate::substrate::{Durability, Substrate};

/// Samples per sealed block. Large on purpose: at ~1 B/sample a 3000-sample
/// block is ~3 KB, so redb's fixed per-key overhead is a rounding error.
const BIG_CHUNK_SAMPLES: usize = 3_000;
/// Fixed sampler cadence assumed by the implicit-timestamp codec.
const STEP_SECS: i64 = 1;

const CHUNKS: TableDefinition<(u32, i64), &[u8]> = TableDefinition::new("compact_chunks");

fn re<E: std::fmt::Display>(e: E) -> TsdbError {
    TsdbError::Redb(e.to_string())
}

/// Substrate B+.
pub struct RedbCompactStore {
    db: Database,
    file: PathBuf,
    open_chunks: BTreeMap<SeriesId, Vec<Sample>>,
    pending: Vec<(SeriesId, i64, Vec<u8>)>,
}

impl RedbCompactStore {
    fn seal(&mut self, series: SeriesId) {
        if let Some(samples) = self.open_chunks.remove(&series) {
            if let Some(first) = samples.first() {
                let first_ts = first.ts;
                let anomaly = vec![false; samples.len()];
                let block = encode_compact(&samples, &anomaly, STEP_SECS);
                self.pending.push((series, first_ts, block));
            }
        }
    }
}

impl Substrate for RedbCompactStore {
    fn open(path: &Path) -> Result<Self> {
        std::fs::create_dir_all(path)?;
        let file = path.join("compact.redb");
        let db = Database::create(&file).map_err(re)?;
        Ok(Self {
            db,
            file,
            open_chunks: BTreeMap::new(),
            pending: Vec::new(),
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
        if self.pending.is_empty() {
            return Ok(());
        }
        let mut wt = self.db.begin_write().map_err(re)?;
        wt.set_durability(match durability {
            Durability::Full => redb::Durability::Immediate,
            Durability::None => redb::Durability::None,
        })
        .map_err(re)?;
        {
            let mut table = wt.open_table(CHUNKS).map_err(re)?;
            for (series, first_ts, block) in self.pending.drain(..) {
                table
                    .insert((series, first_ts), block.as_slice())
                    .map_err(re)?;
            }
        }
        wt.commit().map_err(re)?;
        Ok(())
    }

    fn range(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<Sample>> {
        let mut out = Vec::new();
        let rt = self.db.begin_read().map_err(re)?;
        match rt.open_table(CHUNKS) {
            Ok(table) => {
                for item in table
                    .range((series, i64::MIN)..=(series, i64::MAX))
                    .map_err(re)?
                {
                    let (_k, v) = item.map_err(re)?;
                    let (samples, _bits) = decode_compact(v.value())?;
                    out.extend(samples.into_iter().filter(|s| s.ts >= start && s.ts < end));
                }
            }
            Err(redb::TableError::TableDoesNotExist(_)) => {}
            Err(e) => return Err(re(e)),
        }
        for (_s, _ts, block) in self.pending.iter().filter(|(s, _, _)| *s == series) {
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
        Ok(std::fs::metadata(&self.file).map(|m| m.len()).unwrap_or(0))
    }

    fn total_samples(&self) -> Result<usize> {
        let mut total = 0usize;
        let rt = self.db.begin_read().map_err(re)?;
        match rt.open_table(CHUNKS) {
            Ok(table) => {
                for item in table.iter().map_err(re)? {
                    let (_k, v) = item.map_err(re)?;
                    total += crate::compact::block_count(v.value()) as usize;
                }
            }
            Err(redb::TableError::TableDoesNotExist(_)) => {}
            Err(e) => return Err(re(e)),
        }
        total += self
            .pending
            .iter()
            .map(|(_, _, b)| crate::compact::block_count(b) as usize)
            .sum::<usize>();
        total += self.open_chunks.values().map(Vec::len).sum::<usize>();
        Ok(total)
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
