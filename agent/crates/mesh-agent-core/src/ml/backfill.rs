//! WS-15 reconnect-backfill replay engine (agent side).
//!
//! On reconnect the agent drains its durable WS-14b history to the central store
//! as a **resolution-tiered, recent-first, gradually-drained** flow — never a
//! stampede. Because local data is durable, backfill has no urgency and never
//! loses data; the only questions are order, resolution, and staying inside the
//! server-granted rate.
//!
//! The locked decisions this module encodes:
//! - **Tiered mapping** — the recent window ships as 10 s from T0
//!   ([`BackfillTier::Raw10s`]); older history up to the mid boundary ships as
//!   1 min from T1 ([`BackfillTier::Rollup1m`]); older-still up to retention ships
//!   as 1 hr from T2 ([`BackfillTier::Rollup1h`]). Full-res **1 s raw is never
//!   sent** — it is reachable only via [`answer_local_history`].
//! - **Hybrid order** — the recent window drains first, then the older tiers
//!   oldest-first from a per-tier watermark, so an interrupted drain resumes
//!   cleanly.
//! - **Retention clamp + clock bounds** — a bucket older than `now - retention`
//!   or wildly in the future (beyond `now + skew`) is skipped, never shipped.
//!
//! The engine is pure and synchronous: it reads through a [`TierReader`] (the
//! store's MVCC snapshot in production, a fake in tests) and yields ready-to-send
//! [`PlannedBatch`]es. It never advances the *durable* cursor itself — the caller
//! persists a batch's cursor only after the server acks it, so a dropped
//! connection re-sends from the last durable watermark (idempotent; the server
//! dedups by timestamp).

use std::collections::BTreeMap;

use edge_tsdb::store::TsdbSnapshot;
use edge_tsdb::tier::TierPoint;
use edge_tsdb::{Sample, SeriesId, Tier, TsdbError};
use mesh_protocol::{BackfillSample, BackfillTier, HistoryPoint};

use super::store_sink::series_dim_name;

/// Read side of the local store the backfill engine needs. Implemented by the
/// store's [`TsdbSnapshot`] in production and by an in-memory fake in tests.
pub trait TierReader {
    /// Committed T0 raw samples (+anomaly bit) over `[start, end]`, ascending.
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

/// Seconds in a 10 s / 1 min / 1 hr backfill bucket.
const RAW_STEP: i64 = 10;
const MIN_STEP: i64 = 60;
const HOUR_STEP: i64 = 3600;

/// Tunables for a reconnect-backfill drain. Age bands are half-open by sample
/// age (`now - ts`): `[0, recent)` → Raw10s, `[recent, mid)` → Rollup1m,
/// `[mid, retention)` → Rollup1h, `>= retention` → skipped (on-demand only).
#[derive(Debug, Clone, Copy)]
pub struct BackfillConfig {
    /// Central VM retention window (seconds). Buckets at or beyond this age are
    /// never shipped — they remain reachable only via an on-demand pull.
    pub retention_secs: i64,
    /// Age below which history ships as 10 s from T0.
    pub recent_secs: i64,
    /// Age below which (and at/above `recent_secs`) history ships as 1 min from T1;
    /// at/above this and below `retention_secs` it ships as 1 hr from T2.
    pub mid_secs: i64,
    /// A bucket timestamp beyond `now + future_skew_secs` is a wild clock and is
    /// skipped rather than shipped to a bogus future instant.
    pub future_skew_secs: i64,
    /// Soft cap on samples per batch (drains split into multiple batches).
    pub max_batch_samples: usize,
}

impl Default for BackfillConfig {
    fn default() -> Self {
        Self {
            retention_secs: 90 * 24 * 3600,
            recent_secs: 48 * 3600,
            mid_secs: 30 * 24 * 3600,
            future_skew_secs: 3600,
            max_batch_samples: 1000,
        }
    }
}

/// Durable per-tier resume watermarks: the newest bucket timestamp already
/// shipped-and-acked for each tier, or `None` if a tier has never shipped.
#[derive(Debug, Clone, Copy, Default)]
pub struct BackfillCursors {
    /// Newest 10 s window shipped from T0.
    pub raw10s: Option<i64>,
    /// Newest 1 min bucket shipped from T1.
    pub rollup1m: Option<i64>,
    /// Newest 1 hr bucket shipped from T2.
    pub rollup1h: Option<i64>,
}

impl BackfillCursors {
    fn get(&self, tier: BackfillTier) -> Option<i64> {
        match tier {
            BackfillTier::Raw10s => self.raw10s,
            BackfillTier::Rollup1m => self.rollup1m,
            BackfillTier::Rollup1h => self.rollup1h,
            _ => None,
        }
    }
}

/// A ready-to-send batch of pre-rolled samples for one tier, sorted ascending by
/// timestamp. `cursor` is the newest bucket timestamp in the batch; the caller
/// advances the durable per-tier watermark to it only after the server acks.
#[derive(Debug, Clone, PartialEq)]
pub struct PlannedBatch {
    pub tier: BackfillTier,
    pub samples: Vec<BackfillSample>,
    pub cursor: i64,
}

/// Which tier the drain is currently emitting. Phases run recent-first.
#[derive(Debug, Clone, Copy, PartialEq)]
enum Phase {
    Recent,
    Mid,
    Old,
    Done,
}

impl Phase {
    fn tier(self) -> Option<BackfillTier> {
        match self {
            Phase::Recent => Some(BackfillTier::Raw10s),
            Phase::Mid => Some(BackfillTier::Rollup1m),
            Phase::Old => Some(BackfillTier::Rollup1h),
            Phase::Done => None,
        }
    }

    fn next(self) -> Phase {
        match self {
            Phase::Recent => Phase::Mid,
            Phase::Mid => Phase::Old,
            Phase::Old | Phase::Done => Phase::Done,
        }
    }
}

/// A stateful, recent-first drain over a [`TierReader`]. Call [`next_batch`]
/// until it returns `None`.
///
/// [`next_batch`]: BackfillDrain::next_batch
pub struct BackfillDrain<'a, R: TierReader> {
    reader: &'a R,
    now: i64,
    cfg: BackfillConfig,
    series: &'a [SeriesId],
    cursors: BackfillCursors,
    phase: Phase,
    /// Next timestamp to read from (inclusive) within the current phase.
    pos: i64,
    /// True once `pos` has been initialized for the current phase.
    pos_ready: bool,
}

impl<'a, R: TierReader> BackfillDrain<'a, R> {
    /// Start a drain from the given durable cursors.
    pub fn new(
        reader: &'a R,
        now: i64,
        cfg: BackfillConfig,
        series: &'a [SeriesId],
        cursors: BackfillCursors,
    ) -> Self {
        Self {
            reader,
            now,
            cfg,
            series,
            cursors,
            phase: Phase::Recent,
            pos: 0,
            pos_ready: false,
        }
    }

    /// Buckets per batch = the sample cap spread across the active series (>=1).
    fn buckets_per_batch(&self) -> i64 {
        let per = self.cfg.max_batch_samples / self.series.len().max(1);
        per.max(1) as i64
    }

    /// The `[lo, hi]` timestamp band and bucket step for a phase.
    fn band(&self, phase: Phase) -> (i64, i64, i64) {
        match phase {
            Phase::Recent => (
                self.now - self.cfg.recent_secs,
                self.now + self.cfg.future_skew_secs,
                RAW_STEP,
            ),
            Phase::Mid => (
                self.now - self.cfg.mid_secs,
                self.now - self.cfg.recent_secs,
                MIN_STEP,
            ),
            Phase::Old => (
                self.now - self.cfg.retention_secs,
                self.now - self.cfg.mid_secs,
                HOUR_STEP,
            ),
            Phase::Done => (0, -1, RAW_STEP),
        }
    }

    /// Whether a bucket `[ts, ts+step)` may ship in the current phase. A rollup
    /// bucket ships only if it lies **entirely** inside its tier's time range, so
    /// a coarse bucket that straddles into a finer tier's range is dropped rather
    /// than double-counting the same wall-clock time at two resolutions. The
    /// finest tier (Raw10s) has no upper straddle to guard, only the recent
    /// floor. Bucket timestamps beyond `now + skew` are wild clocks and never
    /// ship; the retention floor is the Old band's own lower bound.
    fn emit_ok(&self, ts: i64, step: i64) -> bool {
        if ts > self.now + self.cfg.future_skew_secs {
            return false;
        }
        let (lo, hi, _) = self.band(self.phase);
        match self.phase {
            Phase::Recent => ts >= lo,
            Phase::Mid | Phase::Old => ts >= lo && ts + step <= hi,
            Phase::Done => false,
        }
    }

    /// Produce the next batch, or `None` when fully drained. Does not touch the
    /// caller's durable cursor.
    pub fn next_batch(&mut self) -> Result<Option<PlannedBatch>, TsdbError> {
        loop {
            let Some(tier) = self.phase.tier() else {
                return Ok(None);
            };
            let (band_lo, band_hi, step) = self.band(self.phase);

            if !self.pos_ready {
                // Resume strictly after the durable watermark, but never before
                // the band's own floor.
                let resume = self.cursors.get(tier).map(|c| c + step);
                self.pos = resume.map_or(band_lo, |r| r.max(band_lo));
                self.pos_ready = true;
            }

            if self.pos > band_hi {
                self.advance_phase();
                continue;
            }

            let read_end = (self.pos + step * self.buckets_per_batch()).min(band_hi);
            let buckets = self.read_buckets(tier, step, self.pos, read_end)?;

            if buckets.is_empty() {
                // Evicted or empty slice — skip it without stalling, advancing
                // past the window we just scanned.
                if read_end >= band_hi {
                    self.advance_phase();
                } else {
                    self.pos = read_end + step;
                }
                continue;
            }

            let cursor = *buckets.keys().next_back().expect("non-empty");
            let mut samples = Vec::new();
            for (ts, dims) in buckets {
                for (series, value) in dims {
                    if let Some(name) = series_dim_name(series) {
                        samples.push(BackfillSample {
                            name: name.to_string(),
                            ts,
                            value,
                        });
                    }
                }
            }
            self.pos = cursor + step;
            return Ok(Some(PlannedBatch {
                tier,
                samples,
                cursor,
            }));
        }
    }

    fn advance_phase(&mut self) {
        self.phase = self.phase.next();
        self.pos_ready = false;
    }

    /// Read one bounded slice for `tier` over `[start, end]`, returning a map of
    /// bucket-ts → the `(series, value)` pairs at that bucket, capped to
    /// `buckets_per_batch` distinct buckets and filtered by [`sanitize`].
    ///
    /// [`sanitize`]: BackfillDrain::sanitize
    fn read_buckets(
        &self,
        tier: BackfillTier,
        step: i64,
        start: i64,
        end: i64,
    ) -> Result<BTreeMap<i64, Vec<(SeriesId, f64)>>, TsdbError> {
        let mut acc: BTreeMap<i64, Vec<(SeriesId, f64)>> = BTreeMap::new();
        for &series in self.series {
            let points: Vec<(i64, f64)> = match tier {
                BackfillTier::Raw10s => roll_to_10s(&self.reader.range_raw(series, start, end)?),
                BackfillTier::Rollup1m => self
                    .reader
                    .range_tier(series, Tier::T1, start, end)?
                    .into_iter()
                    .map(|p| (p.bucket, p.avg))
                    .collect(),
                BackfillTier::Rollup1h => self
                    .reader
                    .range_tier(series, Tier::T2, start, end)?
                    .into_iter()
                    .map(|p| (p.bucket, p.avg))
                    .collect(),
                _ => Vec::new(),
            };
            for (ts, value) in points {
                if self.emit_ok(ts, step) {
                    acc.entry(ts).or_default().push((series, value));
                }
            }
        }
        // Cap to buckets_per_batch distinct bucket timestamps (ascending).
        let cap = self.buckets_per_batch() as usize;
        if acc.len() > cap {
            let keep: Vec<i64> = acc.keys().take(cap).copied().collect();
            acc.retain(|k, _| keep.contains(k));
        }
        Ok(acc)
    }
}

/// Fold 1 s raw samples into 10 s-window averages (window start = `floor(ts/10)*10`),
/// ascending. A partial window (fewer than 10 samples, e.g. across an offline
/// gap) averages the samples that exist — the best available value.
fn roll_to_10s(samples: &[(Sample, bool)]) -> Vec<(i64, f64)> {
    let mut acc: BTreeMap<i64, (f64, u32)> = BTreeMap::new();
    for (s, _) in samples {
        let window = s.ts.div_euclid(RAW_STEP) * RAW_STEP;
        let e = acc.entry(window).or_insert((0.0, 0));
        e.0 += s.value;
        e.1 += 1;
    }
    acc.into_iter()
        .map(|(w, (sum, n))| (w, sum / f64::from(n)))
        .collect()
}

/// Answer an on-demand deep-history pull: full-resolution 1 s T0 raw for `series`
/// over `[from, to]`, capped at `max_points`. Returns the ascending points and a
/// `truncated` flag set when the window held more than `max_points` samples.
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
    fn roll_to_10s_averages_each_window() {
        let samples: Vec<(Sample, bool)> = (0..25)
            .map(|ts| (Sample::new(ts, ts as f64), false))
            .collect();
        let rolled = roll_to_10s(&samples);
        assert_eq!(
            rolled,
            vec![(0, 4.5), (10, 14.5), (20, 22.0)],
            "each 10 s window averages its 1 s samples"
        );
    }

    #[test]
    fn roll_to_10s_handles_a_partial_window() {
        let samples = vec![
            (Sample::new(30, 10.0), false),
            (Sample::new(31, 20.0), false),
        ];
        assert_eq!(roll_to_10s(&samples), vec![(30, 15.0)]);
    }
}
