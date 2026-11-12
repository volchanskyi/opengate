//! Raw blocks and their labels: each block's earliest and latest second, kept beside it, so a read
//! decodes only the blocks that reach into its span.

use redb::{ReadTransaction, ReadableTable, TableDefinition, TableError, WriteTransaction};

use super::blocks::{re, upsert_block, OpenBlockTable, T0};
use crate::compact::decode_compact;
use crate::error::Result;
use crate::sample::{Sample, SeriesId};

/// Each raw block's earliest and latest second, by the block's key.
pub(super) const T0_SPAN: TableDefinition<(u32, i64), (i64, i64)> = TableDefinition::new("t0_span");
/// Raw blocks by `(series, latest second, key)` to their earliest second, so a read opens only
/// blocks ending at or after its start; a clock set back widens a block's span, never hides it.
pub(super) const T0_BY_END: TableDefinition<(u32, i64, i64), i64> =
    TableDefinition::new("t0_by_end");

/// The raw blocks and their two labels, open in one write transaction.
pub(super) struct RawTables<'txn> {
    blocks: OpenBlockTable<'txn>,
    span: redb::Table<'txn, (u32, i64), (i64, i64)>,
    by_end: redb::Table<'txn, (u32, i64, i64), i64>,
}

impl<'txn> RawTables<'txn> {
    pub(super) fn open(wt: &'txn WriteTransaction) -> Result<Self> {
        Ok(Self {
            blocks: wt.open_table(T0).map_err(re)?,
            span: wt.open_table(T0_SPAN).map_err(re)?,
            by_end: wt.open_table(T0_BY_END).map_err(re)?,
        })
    }

    /// Writes one raw block and relabels it with the span of its samples.
    pub(super) fn put(
        &mut self,
        key: (u32, i64),
        bytes: &[u8],
        samples: &[(Sample, bool)],
        logical: &mut u64,
    ) -> Result<()> {
        upsert_block(&mut self.blocks, key, bytes, logical)?;
        self.unlabel(key)?;
        if let Some(span) = ts_span(samples.iter().map(|(s, _)| s.ts)) {
            self.label(key, span)?;
        }
        Ok(())
    }

    fn label(&mut self, key: (u32, i64), (earliest, latest): (i64, i64)) -> Result<()> {
        self.span.insert(key, (earliest, latest)).map_err(re)?;
        self.by_end
            .insert((key.0, latest, key.1), earliest)
            .map_err(re)?;
        Ok(())
    }

    fn unlabel(&mut self, key: (u32, i64)) -> Result<()> {
        let old = self.span.remove(key).map_err(re)?.map(|g| g.value());
        if let Some((_, latest)) = old {
            self.by_end.remove((key.0, latest, key.1)).map_err(re)?;
        }
        Ok(())
    }

    /// Removes one raw block and its labels, returning the bytes reclaimed.
    pub(super) fn remove(&mut self, key: (u32, i64)) -> Result<u64> {
        let removed = self.blocks.remove(key).map_err(re)?;
        let bytes = removed.map(|g| g.value().len() as u64).unwrap_or(0);
        self.unlabel(key)?;
        Ok(bytes)
    }
}

/// The earliest and latest of `timestamps`, or `None` when there are none.
fn ts_span(timestamps: impl IntoIterator<Item = i64>) -> Option<(i64, i64)> {
    timestamps.into_iter().fold(None, |acc, ts| match acc {
        None => Some((ts, ts)),
        Some((lo, hi)) => Some((lo.min(ts), hi.max(ts))),
    })
}

/// A raw block's key and its stored bytes.
type RawBlock = ((u32, i64), Vec<u8>);

/// The raw blocks a store written before labels holds without one, with their bytes.
fn unlabelled_blocks(wt: &WriteTransaction) -> Result<Vec<RawBlock>> {
    let blocks = wt.open_table(T0).map_err(re)?;
    let span = wt.open_table(T0_SPAN).map_err(re)?;
    let mut out = Vec::new();
    for item in blocks.iter().map_err(re)? {
        let (key, bytes) = item.map_err(re)?;
        if span.get(key.value()).map_err(re)?.is_none() {
            out.push((key.value(), bytes.value().to_vec()));
        }
    }
    Ok(out)
}

/// Labels every raw block that has no label yet, decoding each once.
pub(super) fn label_unlabelled(wt: &WriteTransaction) -> Result<()> {
    let unlabelled = unlabelled_blocks(wt)?;
    let mut raw = RawTables::open(wt)?;
    for (key, bytes) in unlabelled {
        let (samples, _) = decode_compact(&bytes)?;
        if let Some(span) = ts_span(samples.iter().map(|s| s.ts)) {
            raw.label(key, span)?;
        }
    }
    Ok(())
}

/// A table of a read snapshot, or `None` when the store has never written it.
fn optional<T>(table: std::result::Result<T, TableError>) -> Result<Option<T>> {
    match table {
        Ok(t) => Ok(Some(t)),
        Err(TableError::TableDoesNotExist(_)) => Ok(None),
        Err(e) => Err(re(e)),
    }
}

/// The keys of `series`' raw blocks whose span reaches into `start..end`.
fn overlapping(rt: &ReadTransaction, series: SeriesId, start: i64, end: i64) -> Result<Vec<i64>> {
    let Some(by_end) = optional(rt.open_table(T0_BY_END))? else {
        return Ok(Vec::new());
    };
    let mut keys = Vec::new();
    for item in by_end
        .range((series, start, i64::MIN)..=(series, i64::MAX, i64::MAX))
        .map_err(re)?
    {
        let (key, earliest) = item.map_err(re)?;
        if earliest.value() < end {
            keys.push(key.value().2);
        }
    }
    Ok(keys)
}

/// Reads raw samples over `start..end`, decoding only the blocks whose span reaches into it.
pub(super) fn read_raw(
    rt: &ReadTransaction,
    series: SeriesId,
    start: i64,
    end: i64,
) -> Result<Vec<(Sample, bool)>> {
    let mut out = Vec::new();
    if start >= end {
        return Ok(out);
    }
    let keys = overlapping(rt, series, start, end)?;
    let Some(blocks) = optional(rt.open_table(T0))? else {
        return Ok(out);
    };
    for first_ts in keys {
        let Some(block) = blocks.get((series, first_ts)).map_err(re)? else {
            continue;
        };
        let (samples, bits) = decode_compact(block.value())?;
        out.extend(
            samples
                .into_iter()
                .zip(bits)
                .filter(|(s, _)| (start..end).contains(&s.ts)),
        );
    }
    out.sort_by_key(|(s, _)| s.ts);
    Ok(out)
}
