//! `LocalTsdb` — the production agent-local multi-tier persistent store.
//!
//! Built by *graduating* the WS-14a spike: the [`compact`](crate::compact) block
//! codec (fixed-point-per-metric + implicit timestamps + inline anomaly bit),
//! the [`tier`](crate::tier) rollups, and the big-block-in-`redb` technique
//! measured in [`redb_compact`](crate::redb_compact). Because central
//! VictoriaMetrics keeps `avg` only, this store is the **sole** home for
//! min/max/last + 1 s raw, so it is load-bearing and its robustness (crash
//! safety, corruption recovery, disk-cap) is inherited from `redb`, not owned.
//!
//! ## Tiers, one atomic transaction
//!
//! - **T0** — 1 s raw samples + inline anomaly bits, packed into big compact
//!   blocks so `redb`'s per-key B-tree overhead amortises.
//! - **T1 / T2** — 60 s / 3600 s rollups (min/max/avg/last/count), keyed by
//!   **sample** timestamp so an NTP step never misbuckets, merged incrementally
//!   so a bucket split across commits still folds exactly.
//!
//! Each commit writes T0 + its T1/T2 rollups in **one** `redb` transaction, so a
//! chunk and its rollups land or roll back together. `Durability::Full` maps to
//! `redb`'s `Immediate` (fsync) commit; `None` is the buffered fast path. The
//! block-I/O and transaction glue lives in [`blocks`].

mod blocks;

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use redb::{Database, ReadTransaction, ReadableDatabase, ReadableTable};

use crate::config::{Durability, TsdbConfig};
use crate::error::{Result, TsdbError};
use crate::sample::{Sample, SeriesId};
use crate::tier::TierPoint;

#[cfg(feature = "cold-deflate")]
use blocks::deflate_cold_blocks;
use blocks::{
    evict_blocks, map_durability, migrate_and_stamp, read_raw, read_stored_version, read_tier,
    scan_logical, write_all, BlockTable, Cand, OpenSeries, CURRENT_FORMAT, CURSOR, T0,
    T0_BLOCK_SAMPLES, T1, T2,
};

/// The two downsampled rollup tiers the store serves alongside T0 raw.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum Tier {
    /// 60 s rollups.
    T1,
    /// 3600 s rollups.
    T2,
}

impl Tier {
    fn table(self) -> BlockTable {
        match self {
            Tier::T1 => T1,
            Tier::T2 => T2,
        }
    }
}

/// The production agent-local multi-tier store.
pub struct LocalTsdb {
    db: Database,
    file: PathBuf,
    config: TsdbConfig,
    host_free: Option<u64>,
    scales: BTreeMap<SeriesId, i64>,
    open: BTreeMap<SeriesId, OpenSeries>,
    logical_bytes: u64,
    format_version: u64,
}

impl LocalTsdb {
    /// Open (creating if absent) a store under `path`, migrating an older format
    /// forward. Returns an error — never a panic — on a store written by a newer
    /// agent than this one understands.
    pub fn open(path: &Path, config: TsdbConfig) -> Result<Self> {
        std::fs::create_dir_all(path)?;
        let file = path.join("localtsdb.redb");
        let db = Database::create(&file).map_err(blocks::re)?;

        let stored = read_stored_version(&db)?;
        if stored.is_some_and(|v| v > CURRENT_FORMAT) {
            return Err(TsdbError::UnsupportedFormat {
                found: stored.unwrap_or(0),
                supported: CURRENT_FORMAT,
            });
        }
        if stored.is_none_or(|v| v < CURRENT_FORMAT) {
            migrate_and_stamp(&db, stored)?;
        }

        let logical_bytes = scan_logical(&db)?;
        Ok(Self {
            db,
            file,
            config,
            host_free: None,
            scales: BTreeMap::new(),
            open: BTreeMap::new(),
            logical_bytes,
            format_version: CURRENT_FORMAT,
        })
    }

    /// Set the fixed-point quantization scale for a series (e.g. `100` for
    /// centi-precision percentages). Applied at encode time; blocks self-describe
    /// their scale, so a policy change never breaks already-written blocks.
    pub fn set_scale(&mut self, series: SeriesId, scale: i64) {
        self.scales.insert(series, scale);
    }

    /// Report currently-free host-disk bytes so the store can back off its cap
    /// under host pressure (`cap = min(cap_bytes, free × host_free_fraction)`).
    /// The store never queries the OS itself, keeping it dependency-free.
    pub fn set_host_free_bytes(&mut self, free: Option<u64>) {
        self.host_free = free;
    }

    /// The effective cap: the configured cap, further limited by host free space.
    fn effective_cap(&self) -> u64 {
        match self.host_free {
            Some(free) if self.config.host_free_fraction > 0.0 => {
                let borrow = (free as f64 * self.config.host_free_fraction) as u64;
                self.config.cap_bytes.min(borrow)
            }
            _ => self.config.cap_bytes,
        }
    }

    /// The store's logical footprint (sum of stored block bytes).
    #[must_use]
    pub fn logical_bytes(&self) -> u64 {
        self.logical_bytes
    }

    /// The on-disk format version this store was migrated to.
    #[must_use]
    pub fn format_version(&self) -> u64 {
        self.format_version
    }

    /// Append one raw sample with its anomaly bit. Buffered until [`commit`].
    ///
    /// [`commit`]: LocalTsdb::commit
    pub fn append(&mut self, series: SeriesId, sample: Sample, anomaly: bool) -> Result<()> {
        let os = self.open.entry(series).or_default();
        if os.tail.is_empty() {
            os.tail_first_ts = sample.ts;
        }
        os.tail.push((sample, anomaly));
        os.unrolled.push(sample);
        if os.tail.len() >= T0_BLOCK_SAMPLES {
            let block = std::mem::take(&mut os.tail);
            os.sealed.push((os.tail_first_ts, block));
        }
        Ok(())
    }

    /// Seal buffered T0 blocks and their T1/T2 rollups and persist them in one
    /// atomic transaction, then enforce the disk cap.
    pub fn commit(&mut self, durability: Durability) -> Result<()> {
        let mut wt = self.db.begin_write().map_err(blocks::re)?;
        wt.set_durability(map_durability(durability))
            .map_err(blocks::re)?;
        let default_scale = self.config.default_scale;
        let logical = write_all(
            &wt,
            &mut self.open,
            &self.scales,
            default_scale,
            self.logical_bytes,
        )?;
        wt.commit().map_err(blocks::re)?;
        self.logical_bytes = logical;
        self.open.retain(|_, os| {
            !os.tail.is_empty() || !os.sealed.is_empty() || !os.unrolled.is_empty()
        });
        self.enforce_cap()
    }

    /// Range-query committed T0 raw samples with their anomaly bits, ascending by
    /// timestamp. Uncommitted (in-flight) samples are intentionally excluded — a
    /// read is a consistent view of durable state.
    pub fn range_raw(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<(Sample, bool)>> {
        let rt = self.db.begin_read().map_err(blocks::re)?;
        read_raw(&rt, series, start, end)
    }

    /// Range-query a rollup tier, ascending by bucket.
    pub fn range_tier(
        &self,
        series: SeriesId,
        tier: Tier,
        start: i64,
        end: i64,
    ) -> Result<Vec<TierPoint>> {
        let rt = self.db.begin_read().map_err(blocks::re)?;
        read_tier(&rt, tier, series, start, end)
    }

    /// The durable backfill cursor for `series` (the last timestamp WS-15 shipped
    /// centrally), or `None` if never set.
    pub fn cursor(&self, series: SeriesId) -> Result<Option<i64>> {
        let rt = self.db.begin_read().map_err(blocks::re)?;
        match rt.open_table(CURSOR) {
            Ok(t) => Ok(t.get(series).map_err(blocks::re)?.map(|g| g.value())),
            Err(redb::TableError::TableDoesNotExist(_)) => Ok(None),
            Err(e) => Err(blocks::re(e)),
        }
    }

    /// Advance and persist the durable backfill cursor for `series`.
    pub fn set_cursor(&mut self, series: SeriesId, ts: i64, durability: Durability) -> Result<()> {
        let mut wt = self.db.begin_write().map_err(blocks::re)?;
        wt.set_durability(map_durability(durability))
            .map_err(blocks::re)?;
        {
            let mut t = wt.open_table(CURSOR).map_err(blocks::re)?;
            t.insert(series, ts).map_err(blocks::re)?;
        }
        wt.commit().map_err(blocks::re)?;
        Ok(())
    }

    /// Open a consistent MVCC read snapshot. Reads on the snapshot see the store
    /// as of this call and are unaffected by concurrent writes — the WS-15 /
    /// detection read-while-the-sampler-writes path is free.
    pub fn snapshot(&self) -> Result<TsdbSnapshot> {
        Ok(TsdbSnapshot {
            rt: self.db.begin_read().map_err(blocks::re)?,
        })
    }

    /// DEFLATE every sealed (non-tail) T1/T2 block to reclaim cold-tier space.
    /// Never touches T0 raw. Opt-in and idempotent; the caller gates it on the
    /// agent's <1 % CPU budget. A no-op when the `cold-deflate` feature is off.
    #[cfg(feature = "cold-deflate")]
    pub fn compact_cold_tiers(&mut self) -> Result<()> {
        let mut logical = self.logical_bytes;
        let mut wt = self.db.begin_write().map_err(blocks::re)?;
        wt.set_durability(redb::Durability::Immediate)
            .map_err(blocks::re)?;
        {
            for def in [T1, T2] {
                let mut table = wt.open_table(def).map_err(blocks::re)?;
                deflate_cold_blocks(&mut table, &mut logical)?;
            }
        }
        wt.commit().map_err(blocks::re)?;
        self.logical_bytes = logical;
        Ok(())
    }

    /// No-op cold-tier compaction when DEFLATE is compiled out.
    #[cfg(not(feature = "cold-deflate"))]
    pub fn compact_cold_tiers(&mut self) -> Result<()> {
        Ok(())
    }

    /// Purge the entire local store — every tier, rollup, and cursor — on
    /// deprovision (WS-20). In-memory buffers are dropped too.
    pub fn purge(&mut self) -> Result<()> {
        let mut wt = self.db.begin_write().map_err(blocks::re)?;
        wt.set_durability(redb::Durability::Immediate)
            .map_err(blocks::re)?;
        for def in [T0, T1, T2] {
            wt.delete_table(def).map_err(blocks::re)?;
        }
        wt.delete_table(CURSOR).map_err(blocks::re)?;
        wt.commit().map_err(blocks::re)?;
        self.open.clear();
        self.logical_bytes = 0;
        Ok(())
    }

    /// On-disk file size in bytes.
    pub fn size_on_disk(&self) -> Result<u64> {
        Ok(std::fs::metadata(&self.file).map(|m| m.len()).unwrap_or(0))
    }

    /// Evict oldest-first (coarsest tier breaking ties) until the store is under
    /// its effective cap. Bounds file growth because redb reuses the freed pages.
    fn enforce_cap(&mut self) -> Result<()> {
        let cap = self.effective_cap();
        if self.logical_bytes <= cap {
            return Ok(());
        }
        let mut cands = self.collect_evictable()?;
        // Always retain the newest raw block so live sampling is never evicted.
        let newest_t0 = cands
            .iter()
            .filter(|c| c.rank == 2)
            .map(|c| (c.ts, c.series))
            .max();
        // Evict globally-oldest first; the coarsest tier (lowest rank) breaks a
        // timestamp tie.
        cands.sort_by_key(|c| (c.ts, c.rank));

        let mut wt = self.db.begin_write().map_err(blocks::re)?;
        wt.set_durability(redb::Durability::Immediate)
            .map_err(blocks::re)?;
        self.logical_bytes = evict_blocks(&wt, cands, cap, newest_t0, self.logical_bytes)?;
        wt.commit().map_err(blocks::re)?;
        Ok(())
    }

    /// Every stored block as an eviction candidate (`ts` = block start, `rank`
    /// T2 = 0 < T1 = 1 < T0 = 2).
    fn collect_evictable(&self) -> Result<Vec<Cand>> {
        let rt = self.db.begin_read().map_err(blocks::re)?;
        let mut cands: Vec<Cand> = Vec::new();
        for (def, rank) in [(T2, 0u8), (T1, 1u8), (T0, 2u8)] {
            if let Ok(t) = rt.open_table(def) {
                for item in t.iter().map_err(blocks::re)? {
                    let (series, ts) = item.map_err(blocks::re)?.0.value();
                    cands.push(Cand { ts, rank, series });
                }
            }
        }
        Ok(cands)
    }
}

/// A consistent MVCC read snapshot of a [`LocalTsdb`].
pub struct TsdbSnapshot {
    rt: ReadTransaction,
}

impl TsdbSnapshot {
    /// Range-query committed T0 raw samples as of the snapshot instant.
    pub fn range_raw(&self, series: SeriesId, start: i64, end: i64) -> Result<Vec<(Sample, bool)>> {
        read_raw(&self.rt, series, start, end)
    }

    /// Range-query a rollup tier as of the snapshot instant.
    pub fn range_tier(
        &self,
        series: SeriesId,
        tier: Tier,
        start: i64,
        end: i64,
    ) -> Result<Vec<TierPoint>> {
        read_tier(&self.rt, tier, series, start, end)
    }
}

#[cfg(test)]
mod tests {
    use super::blocks::{META, META_VERSION};
    use super::{LocalTsdb, CURRENT_FORMAT};
    use crate::config::{Durability, TsdbConfig};
    use crate::error::TsdbError;
    use crate::sample::Sample;

    /// Stamp an arbitrary format version on an existing store's `redb` file,
    /// simulating one written by a different agent build.
    fn stamp_version(dir: &std::path::Path, v: u64) {
        let db = LocalTsdb::open(dir, TsdbConfig::default()).unwrap();
        let wt = db.db.begin_write().unwrap();
        {
            let mut m = wt.open_table(META).unwrap();
            m.insert(META_VERSION, &v).unwrap();
        }
        wt.commit().unwrap();
    }

    #[test]
    fn opens_and_migrates_an_older_format_store() {
        let dir = tempfile::tempdir().unwrap();
        {
            let mut db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
            for i in 0..100 {
                db.append(0, Sample::new(1_000 + i, i as f64), false)
                    .unwrap();
            }
            db.commit(Durability::Full).unwrap();
        }
        // A prior agent stamped an older format; reopening must migrate forward.
        stamp_version(dir.path(), CURRENT_FORMAT - 1);
        let db = LocalTsdb::open(dir.path(), TsdbConfig::default()).unwrap();
        assert_eq!(db.format_version(), CURRENT_FORMAT);
        // The WS-15 backlog is never orphaned by a migration.
        assert_eq!(db.range_raw(0, i64::MIN, i64::MAX).unwrap().len(), 100);
    }

    #[test]
    fn refuses_a_future_format_store() {
        let dir = tempfile::tempdir().unwrap();
        stamp_version(dir.path(), 999);
        // (`unwrap_err` would need `LocalTsdb: Debug`, which holds a redb handle.)
        let result = LocalTsdb::open(dir.path(), TsdbConfig::default());
        assert!(matches!(
            result,
            Err(TsdbError::UnsupportedFormat { found: 999, .. })
        ));
    }
}
