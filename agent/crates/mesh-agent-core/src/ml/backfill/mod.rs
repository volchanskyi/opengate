//! Reconnect backfill: recent-first tiered replay of durable history the server never confirmed;
//! a batch's span is confirmed only once the server acks it, so a dropped link re-sends it.

use std::collections::BTreeMap;
use std::time::Duration;

use edge_tsdb::confirmed::ConfirmedRanges;
use edge_tsdb::store::TsdbSnapshot;
use edge_tsdb::tier::TierPoint;
use edge_tsdb::{Durability, LocalTsdb, Sample, SeriesId, Tier, TsdbError};
use mesh_protocol::HistoryPoint;

use super::store_sink::WindowReduction;

mod drain;

pub use drain::{BackfillDrain, PlannedBatch};

/// Read side of the local store that the backfill engine drains.
pub trait TierReader {
    /// Committed T0 raw samples with the anomaly bit over `[start, end]`, ascending.
    fn range_raw(
        &self,
        series: SeriesId,
        start: i64,
        end: i64,
    ) -> Result<Vec<(Sample, bool)>, TsdbError>;

    /// Committed rollup-tier points over `[start, end]`, ascending by bucket.
    fn range_tier(
        &self,
        series: SeriesId,
        tier: Tier,
        start: i64,
        end: i64,
    ) -> Result<Vec<TierPoint>, TsdbError>;
}

impl TierReader for TsdbSnapshot {
    fn range_raw(
        &self,
        series: SeriesId,
        start: i64,
        end: i64,
    ) -> Result<Vec<(Sample, bool)>, TsdbError> {
        TsdbSnapshot::range_raw(self, series, start, end)
    }

    fn range_tier(
        &self,
        series: SeriesId,
        tier: Tier,
        start: i64,
        end: i64,
    ) -> Result<Vec<TierPoint>, TsdbError> {
        TsdbSnapshot::range_tier(self, series, tier, start, end)
    }
}

/// One bucket's readings for a series: the bucket's average and its maximum.
type BucketReduction = (SeriesId, f64, f64);

/// Seconds in a recent, 1 min and 1 hr backfill bucket; the recent tier uses the live 60 s grid.
const RECENT_STEP: i64 = 60;
const MIN_STEP: i64 = 60;
const HOUR_STEP: i64 = 3600;

/// Age bands by `now - ts`: up to `recent` ships 60 s, up to `mid` 1 min,
/// up to `retention` 1 hr.
#[derive(Debug, Clone, Copy)]
pub struct BackfillConfig {
    /// Central retention in seconds; buckets older than this are never shipped.
    pub retention_secs: i64,
    /// Age below which history ships as 60 s rolled from T0.
    pub recent_secs: i64,
    /// Age below which history ships as 1 min from T1; older ships as 1 hr from T2.
    pub mid_secs: i64,
    /// Soft cap on samples per batch.
    pub max_batch_samples: usize,
}

impl Default for BackfillConfig {
    fn default() -> Self {
        Self {
            retention_secs: 30 * 24 * 3600,
            recent_secs: 48 * 3600,
            mid_secs: 30 * 24 * 3600,
            max_batch_samples: 1000,
        }
    }
}

/// The 60 s window start (`floor(ts/60)*60`) shared by the live emitter and backfill.
pub(crate) fn window_start_60s(ts: i64) -> i64 {
    ts.div_euclid(RECENT_STEP) * RECENT_STEP
}

/// Folds 1 s samples into ascending 60 s `(window start, value, maximum)`, with `reduction`
/// choosing mean or latest so a backfilled point equals the live one.
pub(crate) fn roll_to_60s(
    samples: &[(Sample, bool)],
    reduction: WindowReduction,
) -> Vec<(i64, f64, f64)> {
    let mut acc: BTreeMap<i64, (f64, f64, u32, i64, f64)> = BTreeMap::new();
    for (s, _) in samples {
        let window = window_start_60s(s.ts);
        let e = acc
            .entry(window)
            .or_insert((0.0, f64::NEG_INFINITY, 0, i64::MIN, 0.0));
        e.0 += s.value;
        e.1 = e.1.max(s.value);
        e.2 += 1;
        if s.ts >= e.3 {
            e.3 = s.ts;
            e.4 = s.value;
        }
    }
    acc.into_iter()
        .map(|(w, (sum, max, n, _, last))| {
            let value = match reduction {
                WindowReduction::Mean => sum / f64::from(n),
                WindowReduction::Last => last,
            };
            (w, value, max)
        })
        .collect()
}

/// The cursor key the 60 s-tier watermark had, read once by [`migrate_cursors`].
pub const TIER_CURSOR_RECENT60S: SeriesId = SeriesId::MAX;
/// The cursor key the 1 min-tier watermark had.
pub const TIER_CURSOR_ROLLUP1M: SeriesId = SeriesId::MAX - 1;
/// The cursor key the 1 hr-tier watermark had.
pub const TIER_CURSOR_ROLLUP1H: SeriesId = SeriesId::MAX - 2;

/// Durable storage for the spans the server confirmed, and the per-tier watermarks they replace.
pub trait ConfirmedStore {
    /// The spans the server has confirmed it stores.
    fn load_confirmed(&self) -> Result<ConfirmedRanges, TsdbError>;
    /// Replaces the confirmed spans.
    fn save_confirmed(&mut self, ranges: &ConfirmedRanges) -> Result<(), TsdbError>;
    /// A watermark an older agent saved under a reserved tier key, or `None`.
    fn load_cursor(&self, key: SeriesId) -> Result<Option<i64>, TsdbError>;
    /// Removes a watermark once it is read into the confirmed spans.
    fn remove_cursor(&mut self, key: SeriesId) -> Result<(), TsdbError>;
}

impl ConfirmedStore for LocalTsdb {
    fn load_confirmed(&self) -> Result<ConfirmedRanges, TsdbError> {
        self.confirmed()
    }

    fn save_confirmed(&mut self, ranges: &ConfirmedRanges) -> Result<(), TsdbError> {
        // Saved without a flush, made durable by the next durable commit; a crash before it
        // sends about a minute again, which the central store keeps as a repeat.
        LocalTsdb::save_confirmed(self, ranges, Durability::None)
    }

    fn load_cursor(&self, key: SeriesId) -> Result<Option<i64>, TsdbError> {
        self.cursor(key)
    }

    fn remove_cursor(&mut self, key: SeriesId) -> Result<(), TsdbError> {
        LocalTsdb::remove_cursor(self, key, Durability::None)
    }
}

/// Records that the server stores `[start, end)`.
pub fn confirm<C: ConfirmedStore>(store: &mut C, start: i64, end: i64) -> Result<(), TsdbError> {
    let mut ranges = store.load_confirmed()?;
    ranges.add(start, end);
    store.save_confirmed(&ranges)
}

/// Forgets every confirmation from the minute holding `since`, so that history is sent again.
pub fn forget_after<C: ConfirmedStore>(store: &mut C, since: i64) -> Result<(), TsdbError> {
    let mut ranges = store.load_confirmed()?;
    ranges.forget_after(since);
    store.save_confirmed(&ranges)
}

/// Turns the per-tier watermarks an older agent kept into confirmed spans once, each tier's band
/// up to its watermark, then removes them; reports whether any were found.
pub fn migrate_cursors<C: ConfirmedStore>(
    store: &mut C,
    now: i64,
    cfg: BackfillConfig,
) -> Result<bool, TsdbError> {
    let tiers = [
        (TIER_CURSOR_RECENT60S, now - cfg.recent_secs, RECENT_STEP),
        (TIER_CURSOR_ROLLUP1M, now - cfg.mid_secs, MIN_STEP),
        (TIER_CURSOR_ROLLUP1H, now - cfg.retention_secs, HOUR_STEP),
    ];
    let mut ranges = store.load_confirmed()?;
    let mut found = false;
    for (key, band_floor, step) in tiers {
        if let Some(cursor) = store.load_cursor(key)? {
            ranges.add(band_floor.min(cursor), cursor + step);
            found = true;
        }
    }
    if !found {
        return Ok(false);
    }
    store.save_confirmed(&ranges)?;
    for (key, _, _) in tiers {
        store.remove_cursor(key)?;
    }
    Ok(true)
}

/// The pending sample count and oldest pending timestamp (`0` when none), from a throwaway drain.
pub fn pending_hint<R: TierReader>(
    reader: &R,
    now: i64,
    cfg: BackfillConfig,
    series: &[SeriesId],
    confirmed: &ConfirmedRanges,
) -> Result<(u64, i64), TsdbError> {
    let mut drain = BackfillDrain::new(reader, now, cfg, series, confirmed);
    let mut pending: u64 = 0;
    let mut oldest = i64::MAX;
    while let Some(batch) = drain.next_batch()? {
        for s in &batch.samples {
            pending += 1;
            oldest = oldest.min(s.ts);
        }
    }
    Ok((pending, if pending == 0 { 0 } else { oldest }))
}

/// The delay keeping `sample_count` samples within `rate` samples/sec; rate `0` or an empty
/// batch waits zero.
#[must_use]
pub fn pace_delay(sample_count: usize, rate: u32) -> Duration {
    if rate == 0 || sample_count == 0 {
        return Duration::ZERO;
    }
    Duration::from_secs_f64(sample_count as f64 / f64::from(rate))
}

/// Returns 1 s raw points for `series` over `[from, to]`, capped at `max_points`, and whether
/// the window was truncated.
pub fn answer_local_history<R: TierReader>(
    reader: &R,
    series: SeriesId,
    from: i64,
    to: i64,
    max_points: usize,
) -> Result<(Vec<HistoryPoint>, bool), TsdbError> {
    let raw = reader.range_raw(series, from, to)?;
    let truncated = raw.len() > max_points;
    let points = raw
        .into_iter()
        .take(max_points)
        .map(|(s, _)| HistoryPoint {
            ts: s.ts,
            value: s.value,
        })
        .collect();
    Ok((points, truncated))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn roll_to_60s_reduces_each_window_to_an_average_and_a_maximum() {
        let samples: Vec<(Sample, bool)> = (0..150)
            .map(|ts| (Sample::new(ts, ts as f64), false))
            .collect();
        let rolled = roll_to_60s(&samples, WindowReduction::Mean);
        assert_eq!(
            rolled,
            vec![(0, 29.5, 59.0), (60, 89.5, 119.0), (120, 134.5, 149.0)],
            "each 60 s window averages its 1 s samples and keeps the largest"
        );
    }

    #[test]
    fn roll_to_60s_publishes_the_latest_reading_for_a_stall_vital() {
        let samples: Vec<(Sample, bool)> = (0..150)
            .map(|ts| (Sample::new(ts, ts as f64), false))
            .collect();
        let rolled = roll_to_60s(&samples, WindowReduction::Last);
        assert_eq!(
            rolled,
            vec![(0, 59.0, 59.0), (60, 119.0, 119.0), (120, 149.0, 149.0)],
            "each 60 s window publishes its latest 1 s reading"
        );
    }

    #[test]
    fn roll_to_60s_latest_reading_is_by_timestamp_not_arrival() {
        let samples = vec![
            (Sample::new(30, 7.0), false),
            (Sample::new(59, 9.0), false),
            (Sample::new(45, 3.0), false),
        ];
        assert_eq!(
            roll_to_60s(&samples, WindowReduction::Last),
            vec![(0, 9.0, 9.0)]
        );
    }

    #[test]
    fn roll_to_60s_handles_a_partial_window() {
        let samples = vec![
            (Sample::new(180, 10.0), false),
            (Sample::new(181, 20.0), false),
        ];
        assert_eq!(
            roll_to_60s(&samples, WindowReduction::Mean),
            vec![(180, 15.0, 20.0)]
        );
        assert_eq!(
            roll_to_60s(&samples, WindowReduction::Last),
            vec![(180, 20.0, 20.0)]
        );
    }

    #[test]
    fn roll_to_60s_maximum_of_negative_readings_is_negative() {
        let samples = vec![
            (Sample::new(0, -30.0), false),
            (Sample::new(1, -10.0), false),
        ];
        assert_eq!(
            roll_to_60s(&samples, WindowReduction::Mean),
            vec![(0, -20.0, -10.0)]
        );
    }
}
