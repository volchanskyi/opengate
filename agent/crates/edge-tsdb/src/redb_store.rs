//! Substrate B — the shared Gorilla blocks persisted in a redb table.
//!
//! redb is a pure-Rust, zero-dependency, MIT/Apache COW B-tree with a two-phase
//! commit and a built-in integrity check. It contributes the crash-safety code
//! we would otherwise own in substrate A, at the cost of B-tree storage overhead
//! per chunk. Chunks seal in memory and are written in one transaction per
//! commit; `Durability::Full` maps to redb's `Immediate` (fsync) commit and
//! `None` to its buffered commit.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use redb::{Database, ReadableDatabase, ReadableTable, TableDefinition};

use crate::error::{Result, TsdbError};
use crate::gorilla::encode_block;
use crate::sample::{Sample, SeriesId};
use crate::substrate::{Durability, Substrate};

/// Samples per sealed chunk (2 minutes at 1 Hz) — matches substrate A so the
/// comparison isolates storage overhead.
const CHUNK_SAMPLES: usize = 120;

/// `(series, first_ts) -> gorilla block`. Tuple keys sort lexicographically, so
/// a single series' chunks form a contiguous, ordered range.
const CHUNKS: TableDefinition<(u32, i64), &[u8]> = TableDefinition::new("chunks");

fn re<E: std::fmt::Display>(e: E) -> TsdbError {
    TsdbError::Redb(e.to_string())
}

/// Substrate B.
pub struct RedbStore {
    db: Database,
    file: PathBuf,
    open_chunks: BTreeMap<SeriesId, Vec<Sample>>,
    pending: Vec<(SeriesId, i64, Vec<u8>)>,
}

impl RedbStore {
    fn seal(&mut self, series: SeriesId) {
        if let Some(samples) = self.open_chunks.remove(&series) {
            if let Some(first) = samples.first() {
                let first_ts = first.ts;
                self.pending
                    .push((series, first_ts, encode_block(&samples)));
            }
        }
    }

    fn for_each_block<F: FnMut(&[u8])>(&self, series: SeriesId, mut f: F) -> Result<()> {
        let rt = self.db.begin_read().map_err(re)?;
        let table = match rt.open_table(CHUNKS) {
            Ok(t) => t,
            // Table absent until the first commit: nothing persisted yet.
            Err(redb::TableError::TableDoesNotExist(_)) => return Ok(()),
            Err(e) => return Err(re(e)),
        };
        for item in table
            .range((series, i64::MIN)..=(series, i64::MAX))
            .map_err(re)?
        {
            let (_k, v) = item.map_err(re)?;
            f(v.value());
        }
        Ok(())
    }
}

impl Substrate for RedbStore {
    fn open(path: &Path) -> Result<Self> {
        std::fs::create_dir_all(path)?;
        let file = path.join("store.redb");
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
        self.for_each_block(series, |block| {
            if let Ok(samples) = crate::gorilla::decode_block(block) {
                out.extend(samples.into_iter().filter(|s| s.ts >= start && s.ts < end));
            }
        })?;
        for (s, _ts, block) in self.pending.iter().filter(|(s, _, _)| *s == series) {
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
        Ok(std::fs::metadata(&self.file).map(|m| m.len()).unwrap_or(0))
    }

    fn total_samples(&self) -> Result<usize> {
        let mut total = 0usize;
        let rt = self.db.begin_read().map_err(re)?;
        match rt.open_table(CHUNKS) {
            Ok(table) => {
                for item in table.iter().map_err(re)? {
                    let (_k, v) = item.map_err(re)?;
                    total += crate::gorilla::block_count(v.value()) as usize;
                }
            }
            Err(redb::TableError::TableDoesNotExist(_)) => {}
            Err(e) => return Err(re(e)),
        }
        total += self
            .pending
            .iter()
            .map(|(_, _, b)| crate::gorilla::block_count(b) as usize)
            .sum::<usize>();
        total += self.open_chunks.values().map(Vec::len).sum::<usize>();
        Ok(total)
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
