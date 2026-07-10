//! Block-I/O and `redb` transaction glue for [`LocalTsdb`](super::LocalTsdb).
//!
//! These free functions do the actual encode/decode, tier merging, cap eviction,
//! and cold-tier DEFLATE against open `redb` tables. They take the store's state
//! by reference (not `&mut self`) so they compose with a live write-transaction
//! borrow of the database. Keeping them here leaves [`super`] as a thin
//! lifecycle/API layer.

use std::collections::BTreeMap;

use redb::{Database, ReadTransaction, ReadableDatabase, ReadableTable, TableDefinition};

use super::Tier;
use crate::compact::{decode_compact, encode_compact_scaled};
use crate::config::Durability;
use crate::error::{Result, TsdbError};
use crate::sample::{Sample, SeriesId};
use crate::tier::{
    decode_tier_block, encode_tier_block, stored_rollup, StoredTierPoint, TierPoint, T1_MINUTE,
    T2_HOUR,
};

/// On-disk format version. Bumped when the schema changes; [`super::LocalTsdb::open`]
/// migrates an older store forward and refuses a newer one.
pub(super) const CURRENT_FORMAT: u64 = 1;

/// Fixed 1 Hz sampler cadence assumed by the implicit-timestamp T0 codec.
const STEP_SECS: i64 = 1;
/// Samples per sealed T0 block — large so `redb`'s per-key overhead amortises.
pub(super) const T0_BLOCK_SAMPLES: usize = 3_000;
/// Rollup buckets per T1 block (720 × 60 s = 12 h).
pub(super) const T1_BLOCK_SPAN: i64 = 720;
/// Rollup buckets per T2 block (168 × 3600 s = 7 d).
pub(super) const T2_BLOCK_SPAN: i64 = 168;

/// Tier-block value tag: stored verbatim.
const TIER_PLAIN: u8 = 0;
/// Tier-block value tag: DEFLATE-compressed (cold tier).
const TIER_DEFLATE: u8 = 1;

/// A `(series, timestamp) -> block bytes` tier table (T0/T1/T2 share this shape).
pub(super) type BlockTable = TableDefinition<'static, (u32, i64), &'static [u8]>;
/// An open handle to a [`BlockTable`] within a write transaction.
type OpenBlockTable<'txn> = redb::Table<'txn, (u32, i64), &'static [u8]>;

pub(super) const META: TableDefinition<&str, u64> = TableDefinition::new("meta");
pub(super) const META_VERSION: &str = "format_version";
pub(super) const T0: BlockTable = TableDefinition::new("t0_raw");
pub(super) const T1: BlockTable = TableDefinition::new("t1_min");
pub(super) const T2: BlockTable = TableDefinition::new("t2_hour");
pub(super) const CURSOR: TableDefinition<u32, i64> = TableDefinition::new("cursor");

pub(super) fn re<E: std::fmt::Display>(e: E) -> TsdbError {
    TsdbError::Redb(e.to_string())
}

/// In-memory, not-yet-durable state for one series.
#[derive(Default)]
pub(super) struct OpenSeries {
    /// First-sample timestamp (redb key) of the growing tail T0 block.
    pub(super) tail_first_ts: i64,
    /// The tail T0 block's samples + anomaly bits (rewritten each commit until it
    /// reaches [`T0_BLOCK_SAMPLES`] and rotates into `sealed`).
    pub(super) tail: Vec<(Sample, bool)>,
    /// Full T0 blocks that rotated out of `tail` and await their final write.
    pub(super) sealed: Vec<(i64, Vec<(Sample, bool)>)>,
    /// Samples appended since the last commit, awaiting rollup into T1/T2.
    pub(super) unrolled: Vec<Sample>,
}

/// One block considered for cap eviction (`ts` = block start, `rank` orders
/// coarsest-first on a timestamp tie: T2 = 0 < T1 = 1 < T0 = 2).
pub(super) struct Cand {
    pub(super) ts: i64,
    pub(super) rank: u8,
    pub(super) series: SeriesId,
}

pub(super) fn map_durability(d: Durability) -> redb::Durability {
    match d {
        Durability::Full => redb::Durability::Immediate,
        Durability::None => redb::Durability::None,
    }
}

/// Fold an upsert's old/new value lengths into the running logical-byte counter.
fn replace_len(logical: u64, old: Option<usize>, new: usize) -> u64 {
    logical - old.unwrap_or(0) as u64 + new as u64
}

fn encode_t0_block(block: &[(Sample, bool)], scale: Option<i64>) -> Vec<u8> {
    let samples: Vec<Sample> = block.iter().map(|(s, _)| *s).collect();
    let anomaly: Vec<bool> = block.iter().map(|(_, a)| *a).collect();
    encode_compact_scaled(&samples, &anomaly, STEP_SECS, scale)
}

/// The store's stamped format version, or `None` for a brand-new (empty) store.
pub(super) fn read_stored_version(db: &Database) -> Result<Option<u64>> {
    let rt = db.begin_read().map_err(re)?;
    match rt.open_table(META) {
        Ok(t) => Ok(t.get(META_VERSION).map_err(re)?.map(|g| g.value())),
        Err(redb::TableError::TableDoesNotExist(_)) => Ok(None),
        Err(e) => Err(re(e)),
    }
}

/// Run any migrations from `from` (a fresh store when `None`) up to
/// [`CURRENT_FORMAT`], then stamp the current version — all in one transaction so
/// an interrupted upgrade never leaves a half-migrated store.
pub(super) fn migrate_and_stamp(db: &Database, from: Option<u64>) -> Result<()> {
    let wt = db.begin_write().map_err(re)?;
    {
        let mut meta = wt.open_table(META).map_err(re)?;
        let mut v = from.unwrap_or(CURRENT_FORMAT);
        while v < CURRENT_FORMAT {
            // v → v+1 migration steps. The current sole format is 1; a store
            // stamped 0 has an identical, self-describing block layout, so the
            // step is a metadata re-stamp with no data rewrite.
            v += 1;
        }
        meta.insert(META_VERSION, &CURRENT_FORMAT).map_err(re)?;
    }
    wt.commit().map_err(re)?;
    Ok(())
}

/// Sum the value bytes of every stored tier block — the store's *logical*
/// footprint, which the disk cap bounds (redb reuses freed pages, so the file
/// tracks this plus bounded COW overhead and never grows without bound).
pub(super) fn scan_logical(db: &Database) -> Result<u64> {
    let rt = db.begin_read().map_err(re)?;
    let mut total = 0u64;
    for def in [T0, T1, T2] {
        match rt.open_table(def) {
            Ok(t) => {
                for item in t.iter().map_err(re)? {
                    total += item.map_err(re)?.1.value().len() as u64;
                }
            }
            Err(redb::TableError::TableDoesNotExist(_)) => {}
            Err(e) => return Err(re(e)),
        }
    }
    Ok(total)
}

/// The stored block bytes for one series in `def`, in key order. An absent table
/// (nothing committed yet) yields an empty list rather than an error.
fn series_blocks(rt: &ReadTransaction, def: BlockTable, series: SeriesId) -> Result<Vec<Vec<u8>>> {
    let mut out = Vec::new();
    match rt.open_table(def) {
        Ok(t) => {
            for item in t
                .range((series, i64::MIN)..=(series, i64::MAX))
                .map_err(re)?
            {
                out.push(item.map_err(re)?.1.value().to_vec());
            }
        }
        Err(redb::TableError::TableDoesNotExist(_)) => {}
        Err(e) => return Err(re(e)),
    }
    Ok(out)
}

/// Seal each series' T0 blocks and merge its rollups into T1/T2 within one open
/// write transaction, returning the updated logical-byte total.
pub(super) fn write_all(
    wt: &redb::WriteTransaction,
    open: &mut BTreeMap<SeriesId, OpenSeries>,
    scales: &BTreeMap<SeriesId, i64>,
    default_scale: Option<i64>,
    mut logical: u64,
) -> Result<u64> {
    let mut t0 = wt.open_table(T0).map_err(re)?;
    let mut t1 = wt.open_table(T1).map_err(re)?;
    let mut t2 = wt.open_table(T2).map_err(re)?;
    for (series, os) in open.iter_mut() {
        let series = *series;
        let scale = scales.get(&series).copied().or(default_scale);
        write_t0_blocks(&mut t0, series, os, scale, &mut logical)?;
        let new: Vec<Sample> = os.unrolled.drain(..).collect();
        if !new.is_empty() {
            merge_tier_writes(
                &mut t1,
                series,
                &new,
                T1_MINUTE,
                T1_BLOCK_SPAN,
                &mut logical,
            )?;
            merge_tier_writes(&mut t2, series, &new, T2_HOUR, T2_BLOCK_SPAN, &mut logical)?;
        }
    }
    Ok(logical)
}

/// Write a series' sealed + tail T0 blocks into the open T0 table.
fn write_t0_blocks(
    t0: &mut OpenBlockTable<'_>,
    series: SeriesId,
    os: &mut OpenSeries,
    scale: Option<i64>,
    logical: &mut u64,
) -> Result<()> {
    for (first_ts, block) in os.sealed.drain(..) {
        upsert_block(
            t0,
            (series, first_ts),
            &encode_t0_block(&block, scale),
            logical,
        )?;
    }
    if !os.tail.is_empty() {
        let bytes = encode_t0_block(&os.tail, scale);
        upsert_block(t0, (series, os.tail_first_ts), &bytes, logical)?;
    }
    Ok(())
}

/// Merge a series' new samples into one rollup tier's blocks (open table).
fn merge_tier_writes(
    table: &mut OpenBlockTable<'_>,
    series: SeriesId,
    new: &[Sample],
    interval: i64,
    span: i64,
    logical: &mut u64,
) -> Result<()> {
    for (bkey, ps) in group_partials(new, interval, span) {
        let existing = table
            .get((series, bkey))
            .map_err(re)?
            .map(|g| g.value().to_vec());
        let bytes = merged_tier_bytes(existing.as_deref(), &ps)?;
        upsert_block(table, (series, bkey), &bytes, logical)?;
    }
    Ok(())
}

/// Insert `bytes` at `key`, folding the replaced value's length into `logical`.
fn upsert_block(
    table: &mut OpenBlockTable<'_>,
    key: (u32, i64),
    bytes: &[u8],
    logical: &mut u64,
) -> Result<()> {
    let old = table.insert(key, bytes).map_err(re)?;
    *logical = replace_len(*logical, old.map(|g| g.value().len()), bytes.len());
    Ok(())
}

/// Evict candidates (oldest-first) within an open write transaction until under
/// `cap`, skipping the newest raw block; returns the new logical total.
pub(super) fn evict_blocks(
    wt: &redb::WriteTransaction,
    cands: Vec<Cand>,
    cap: u64,
    newest_t0: Option<(i64, SeriesId)>,
    mut logical: u64,
) -> Result<u64> {
    let mut t0 = wt.open_table(T0).map_err(re)?;
    let mut t1 = wt.open_table(T1).map_err(re)?;
    let mut t2 = wt.open_table(T2).map_err(re)?;
    for c in cands {
        if logical <= cap {
            break;
        }
        let is_newest_raw = c.rank == 2 && newest_t0 == Some((c.ts, c.series));
        if !is_newest_raw {
            logical -= remove_ranked(&mut t0, &mut t1, &mut t2, &c)?;
        }
    }
    Ok(logical)
}

/// Remove one candidate block from its tier table, returning the bytes reclaimed.
fn remove_ranked(
    t0: &mut OpenBlockTable<'_>,
    t1: &mut OpenBlockTable<'_>,
    t2: &mut OpenBlockTable<'_>,
    c: &Cand,
) -> Result<u64> {
    let removed = match c.rank {
        0 => t2.remove((c.series, c.ts)).map_err(re)?,
        1 => t1.remove((c.series, c.ts)).map_err(re)?,
        _ => t0.remove((c.series, c.ts)).map_err(re)?,
    };
    Ok(removed.map(|g| g.value().len() as u64).unwrap_or(0))
}

/// DEFLATE every sealed (non-tail, still-plain) block of one tier table in place.
#[cfg(feature = "cold-deflate")]
pub(super) fn deflate_cold_blocks(table: &mut OpenBlockTable<'_>, logical: &mut u64) -> Result<()> {
    let entries: Vec<((u32, i64), Vec<u8>)> = table
        .iter()
        .map_err(re)?
        .map(|item| {
            item.map(|(k, v)| (k.value(), v.value().to_vec()))
                .map_err(re)
        })
        .collect::<Result<_>>()?;
    let mut max_key: BTreeMap<u32, i64> = BTreeMap::new();
    for ((s, k), _) in &entries {
        max_key
            .entry(*s)
            .and_modify(|m| *m = (*m).max(*k))
            .or_insert(*k);
    }
    for ((s, k), val) in entries {
        // Leave the hot tail block, and already-cold blocks, alone.
        let is_cold_candidate = max_key.get(&s) != Some(&k) && val.first() == Some(&TIER_PLAIN);
        if is_cold_candidate {
            deflate_one(table, (s, k), &val, logical)?;
        }
    }
    Ok(())
}

/// DEFLATE one plain tier block in place, if the compressed form is smaller.
#[cfg(feature = "cold-deflate")]
fn deflate_one(
    table: &mut OpenBlockTable<'_>,
    key: (u32, i64),
    val: &[u8],
    logical: &mut u64,
) -> Result<()> {
    let mut newv = vec![TIER_DEFLATE];
    newv.extend_from_slice(&crate::deflate::deflate(&val[1..])?);
    if newv.len() < val.len() {
        upsert_block(table, key, &newv, logical)?;
    }
    Ok(())
}

pub(super) fn read_raw(
    rt: &ReadTransaction,
    series: SeriesId,
    start: i64,
    end: i64,
) -> Result<Vec<(Sample, bool)>> {
    let mut out = Vec::new();
    for block in series_blocks(rt, T0, series)? {
        let (samples, bits) = decode_compact(&block)?;
        for (s, a) in samples.into_iter().zip(bits) {
            if (start..end).contains(&s.ts) {
                out.push((s, a));
            }
        }
    }
    out.sort_by_key(|(s, _)| s.ts);
    Ok(out)
}

pub(super) fn read_tier(
    rt: &ReadTransaction,
    tier: Tier,
    series: SeriesId,
    start: i64,
    end: i64,
) -> Result<Vec<TierPoint>> {
    let mut out = Vec::new();
    for block in series_blocks(rt, tier.table(), series)? {
        for p in decode_tier_value(&block)? {
            if (start..end).contains(&p.bucket) {
                out.push(p.to_public());
            }
        }
    }
    out.sort_by_key(|p| p.bucket);
    Ok(out)
}

fn block_key(bucket: i64, interval: i64, span: i64) -> i64 {
    let block = interval * span;
    bucket.div_euclid(block) * block
}

fn group_partials(new: &[Sample], interval: i64, span: i64) -> BTreeMap<i64, Vec<StoredTierPoint>> {
    let mut by_block: BTreeMap<i64, Vec<StoredTierPoint>> = BTreeMap::new();
    for p in stored_rollup(new, interval) {
        by_block
            .entry(block_key(p.bucket, interval, span))
            .or_default()
            .push(p);
    }
    by_block
}

/// Merge `partials` into the existing (possibly DEFLATE'd) tier block and
/// re-encode it verbatim (a merged block is hot again, so it is left plain until
/// the next [`super::LocalTsdb::compact_cold_tiers`]).
fn merged_tier_bytes(existing: Option<&[u8]>, partials: &[StoredTierPoint]) -> Result<Vec<u8>> {
    let mut map: BTreeMap<i64, StoredTierPoint> = match existing {
        Some(b) => decode_tier_value(b)?
            .into_iter()
            .map(|p| (p.bucket, p))
            .collect(),
        None => BTreeMap::new(),
    };
    for p in partials {
        map.entry(p.bucket).and_modify(|e| e.merge(p)).or_insert(*p);
    }
    let points: Vec<StoredTierPoint> = map.into_values().collect();
    let mut out = Vec::with_capacity(1 + points.len() * 52);
    out.push(TIER_PLAIN);
    out.extend_from_slice(&encode_tier_block(&points));
    Ok(out)
}

fn decode_tier_value(bytes: &[u8]) -> Result<Vec<StoredTierPoint>> {
    match bytes.first() {
        Some(&TIER_PLAIN) => decode_tier_block(&bytes[1..]),
        Some(&TIER_DEFLATE) => decode_tier_deflate(&bytes[1..]),
        _ => Err(TsdbError::CorruptBlock("tier value tag")),
    }
}

#[cfg(feature = "cold-deflate")]
fn decode_tier_deflate(bytes: &[u8]) -> Result<Vec<StoredTierPoint>> {
    decode_tier_block(&crate::deflate::inflate(bytes)?)
}

#[cfg(not(feature = "cold-deflate"))]
fn decode_tier_deflate(_bytes: &[u8]) -> Result<Vec<StoredTierPoint>> {
    Err(TsdbError::CorruptBlock("deflate feature disabled"))
}
