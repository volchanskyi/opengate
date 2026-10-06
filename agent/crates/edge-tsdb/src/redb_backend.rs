//! The redb substrate [`ChunkedStore`]: per-series chunks sealed through a [`BlockCodec`] and
//! written in one transaction per commit, with `Full` as redb's `Immediate` and `None` as `None`.

use std::collections::BTreeMap;
use std::marker::PhantomData;
use std::path::{Path, PathBuf};

use redb::{Database, ReadableDatabase, ReadableTable, TableDefinition};

use crate::config::Durability;
use crate::error::{Result, TsdbError};
use crate::sample::{Sample, SeriesId};
use crate::substrate::Substrate;

/// An encoded block awaiting the next commit: `(series, first_ts, bytes)`.
pub(crate) type PendingBlock = (SeriesId, i64, Vec<u8>);

/// `(series, first_ts) -> encoded block`. Tuple keys sort lexicographically, so
/// one series' blocks form a contiguous, ordered range.
type ChunkTable = TableDefinition<'static, (u32, i64), &'static [u8]>;

pub(crate) fn re<E: std::fmt::Display>(e: E) -> TsdbError {
    TsdbError::Redb(e.to_string())
}

/// The block encoding a [`ChunkedStore`] seals with, and the file, table and cadence it uses.
pub trait BlockCodec {
    /// File name under the store's directory.
    const FILE: &'static str;
    /// Table holding the encoded blocks.
    const TABLE: &'static str;
    /// Samples buffered per series before a chunk seals.
    const CHUNK_SAMPLES: usize;
    /// True when a stored block that fails to decode fails the read; false skips that block.
    const STRICT: bool;
    /// Encodes one sealed chunk.
    fn encode(samples: &[Sample]) -> Vec<u8>;
    /// Decodes one block.
    fn decode(block: &[u8]) -> Result<Vec<Sample>>;
    /// Samples held in one block.
    fn count(block: &[u8]) -> usize;
}

/// A redb substrate whose per-series chunks seal through codec `C`.
pub struct ChunkedStore<C> {
    backend: RedbBackend,
    open_chunks: BTreeMap<SeriesId, Vec<Sample>>,
    codec: PhantomData<C>,
}

impl<C: BlockCodec> ChunkedStore<C> {
    fn seal(&mut self, series: SeriesId) {
        if let Some(samples) = self.open_chunks.remove(&series) {
            if let Some(first) = samples.first() {
                let first_ts = first.ts;
                self.backend
                    .pending
                    .push((series, first_ts, C::encode(&samples)));
            }
        }
    }
}

impl<C: BlockCodec> Substrate for ChunkedStore<C> {
    fn open(path: &Path) -> Result<Self> {
        Ok(Self {
            backend: RedbBackend::open(path, C::FILE, C::TABLE)?,
            open_chunks: BTreeMap::new(),
            codec: PhantomData,
        })
    }

    fn append(&mut self, series: SeriesId, sample: Sample) -> Result<()> {
        let buf = self.open_chunks.entry(series).or_default();
        buf.push(sample);
        if buf.len() >= C::CHUNK_SAMPLES {
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
        let in_window = |s: &Sample| s.ts >= start && s.ts < end;
        let mut out = Vec::new();
        let mut err = None;
        self.backend
            .for_each_block(series, |block| match C::decode(block) {
                Ok(samples) => out.extend(samples.into_iter().filter(in_window)),
                Err(e) if C::STRICT => err = Some(e),
                Err(_) => {}
            })?;
        if let Some(e) = err {
            return Err(e);
        }
        for (_s, _ts, block) in self.backend.pending.iter().filter(|(s, _, _)| *s == series) {
            out.extend(C::decode(block)?.into_iter().filter(in_window));
        }
        if let Some(buf) = self.open_chunks.get(&series) {
            out.extend(buf.iter().copied().filter(in_window));
        }
        out.sort_by_key(|s| s.ts);
        Ok(out)
    }

    fn size_on_disk(&self) -> Result<u64> {
        self.backend.size_on_disk()
    }

    fn total_samples(&self) -> Result<usize> {
        let open = self.open_chunks.values().map(Vec::len).sum::<usize>();
        self.backend.total_samples(C::count, open)
    }
}

/// The redb database handle, its file and table, and the blocks staged for the next commit.
pub(crate) struct RedbBackend {
    db: Database,
    file: PathBuf,
    table: ChunkTable,
    /// Blocks sealed by the substrate and drained on commit.
    pub(crate) pending: Vec<PendingBlock>,
}

impl RedbBackend {
    /// Open (creating if absent) a store at `path/filename`, keyed under `table`.
    pub(crate) fn open(path: &Path, filename: &str, table_name: &'static str) -> Result<Self> {
        std::fs::create_dir_all(path)?;
        let file = path.join(filename);
        let db = Database::create(&file).map_err(re)?;
        Ok(Self {
            db,
            file,
            table: TableDefinition::new(table_name),
            pending: Vec::new(),
        })
    }

    /// Drains `pending` into the table in one transaction at the requested durability.
    /// Nothing staged opens no transaction.
    pub(crate) fn write_pending(&mut self, durability: Durability) -> Result<()> {
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
            let mut table = wt.open_table(self.table).map_err(re)?;
            for (series, first_ts, block) in self.pending.drain(..) {
                table
                    .insert((series, first_ts), block.as_slice())
                    .map_err(re)?;
            }
        }
        wt.commit().map_err(re)?;
        Ok(())
    }

    /// Invokes `f` on each stored block of `series` in timestamp order.
    /// The table exists after the first commit, and before it the scan is empty.
    pub(crate) fn for_each_block<F: FnMut(&[u8])>(&self, series: SeriesId, mut f: F) -> Result<()> {
        let rt = self.db.begin_read().map_err(re)?;
        let table = match rt.open_table(self.table) {
            Ok(t) => t,
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

    /// Sums `count` over every stored and pending block, plus the `open_samples` buffered.
    pub(crate) fn total_samples<C: Fn(&[u8]) -> usize>(
        &self,
        count: C,
        open_samples: usize,
    ) -> Result<usize> {
        let mut total = open_samples;
        let rt = self.db.begin_read().map_err(re)?;
        match rt.open_table(self.table) {
            Ok(table) => {
                for item in table.iter().map_err(re)? {
                    let (_k, v) = item.map_err(re)?;
                    total += count(v.value());
                }
            }
            Err(redb::TableError::TableDoesNotExist(_)) => {}
            Err(e) => return Err(re(e)),
        }
        total += self.pending.iter().map(|(_, _, b)| count(b)).sum::<usize>();
        Ok(total)
    }

    /// Bytes resident on disk.
    pub(crate) fn size_on_disk(&self) -> Result<u64> {
        Ok(std::fs::metadata(&self.file).map(|m| m.len()).unwrap_or(0))
    }
}

#[cfg(test)]
mod tests {
    use super::{BlockCodec, ChunkedStore};
    use crate::redb_compact::CompactCodec;
    use crate::redb_store::GorillaCodec;
    use crate::sample::Sample;
    use crate::substrate::{Durability, Substrate};

    const GARBAGE: [u8; 2] = [0xFF, 0xFF];

    fn reads_span_open_sealed_and_committed<C: BlockCodec>() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = ChunkedStore::<C>::open(dir.path()).unwrap();
        let n = C::CHUNK_SAMPLES as i64 + 10;
        for i in 0..n {
            s.append(1, Sample::new(1_000 + i, 10.0 + (i % 4) as f64))
                .unwrap();
        }
        let before = s.range(1, i64::MIN, i64::MAX).unwrap();
        assert_eq!(before.len(), n as usize);
        assert!(before.windows(2).all(|w| w[0].ts < w[1].ts));
        assert_eq!(s.total_samples().unwrap(), n as usize);
        s.commit(Durability::None).unwrap();
        assert_eq!(s.range(1, i64::MIN, i64::MAX).unwrap(), before);
        assert_eq!(s.range(1, 1_010, 1_020).unwrap().len(), 10);
        assert_eq!(s.total_samples().unwrap(), n as usize);
    }

    fn empty_store_reads_clean<C: BlockCodec>() {
        let dir = tempfile::tempdir().unwrap();
        let s = ChunkedStore::<C>::open(dir.path()).unwrap();
        assert_eq!(s.total_samples().unwrap(), 0);
        assert!(s.range(1, 0, 100).unwrap().is_empty());
    }

    fn read_past_stored_garbage<C: BlockCodec>() -> crate::error::Result<Vec<Sample>> {
        let dir = tempfile::tempdir().unwrap();
        let mut s = ChunkedStore::<C>::open(dir.path()).unwrap();
        s.append(1, Sample::new(1_000, 1.0)).unwrap();
        s.backend.pending.push((1, 5_000, GARBAGE.to_vec()));
        s.commit(Durability::None).unwrap();
        s.range(1, i64::MIN, i64::MAX)
    }

    fn pending_garbage_fails_the_read<C: BlockCodec>() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = ChunkedStore::<C>::open(dir.path()).unwrap();
        s.backend.pending.push((1, 5_000, GARBAGE.to_vec()));
        assert!(s.range(1, i64::MIN, i64::MAX).is_err());
    }

    #[test]
    fn gorilla_reads_span_open_sealed_and_committed() {
        reads_span_open_sealed_and_committed::<GorillaCodec>();
    }

    #[test]
    fn compact_reads_span_open_sealed_and_committed() {
        reads_span_open_sealed_and_committed::<CompactCodec>();
    }

    #[test]
    fn gorilla_empty_store_reads_clean() {
        empty_store_reads_clean::<GorillaCodec>();
    }

    #[test]
    fn compact_empty_store_reads_clean() {
        empty_store_reads_clean::<CompactCodec>();
    }

    #[test]
    fn gorilla_skips_a_stored_block_that_fails_to_decode() {
        let got = read_past_stored_garbage::<GorillaCodec>().unwrap();
        assert_eq!(got, vec![Sample::new(1_000, 1.0)]);
    }

    #[test]
    fn compact_fails_a_read_over_a_stored_block_that_fails_to_decode() {
        assert!(read_past_stored_garbage::<CompactCodec>().is_err());
    }

    #[test]
    fn both_codecs_fail_a_read_over_a_pending_block_that_fails_to_decode() {
        pending_garbage_fails_the_read::<GorillaCodec>();
        pending_garbage_fails_the_read::<CompactCodec>();
    }
}
