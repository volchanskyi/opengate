//! Shared redb persistence plumbing for the two redb bake-off substrates.
//!
//! Substrates B ([`RedbStore`](crate::redb_store)) and B+
//! ([`RedbCompactStore`](crate::redb_compact)) differ only in their block codec
//! and seal cadence; the redb open/commit/scan/size machinery is identical. It
//! lives here once so the measured difference between the two is attributable to
//! the codec lever, not to a divergent storage path.

use std::path::{Path, PathBuf};

use redb::{Database, ReadableDatabase, ReadableTable, TableDefinition};

use crate::config::Durability;
use crate::error::{Result, TsdbError};
use crate::sample::SeriesId;

/// A pending, already-encoded block awaiting the next commit: `(series, first_ts, bytes)`.
pub(crate) type PendingBlock = (SeriesId, i64, Vec<u8>);

/// `(series, first_ts) -> encoded block`. Tuple keys sort lexicographically, so
/// one series' blocks form a contiguous, ordered range.
type ChunkTable = TableDefinition<'static, (u32, i64), &'static [u8]>;

pub(crate) fn re<E: std::fmt::Display>(e: E) -> TsdbError {
    TsdbError::Redb(e.to_string())
}

/// The redb-backed store shared by substrates B and B+: the database handle, its
/// file, a per-store table name, and the encoded blocks staged for the next
/// commit.
pub(crate) struct RedbBackend {
    db: Database,
    file: PathBuf,
    table: ChunkTable,
    /// Encoded blocks staged by the substrate's `seal`, drained on commit.
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

    /// Drain `pending` into the table in one transaction at the requested
    /// durability. A no-op (and no empty transaction) when nothing is staged.
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

    /// Invoke `f` on each stored block for `series`, in timestamp order. The
    /// table is absent until the first commit — that reads as empty, not an error.
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

    /// Total recoverable sample count: `count` applied to every stored and
    /// pending block, plus `open_samples` still buffered in the substrate.
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
