//! The tier walk: which phase a drain is in, the timestamp band that phase
//! covers, and where one batch ends and the next begins.
//!
//! A drain runs recent-first and then oldest-first per tier from a durable
//! watermark, so an interrupted replay resumes without re-sending and without
//! shipping the same wall-clock time at two resolutions. It is pure and
//! synchronous: it reads through a [`TierReader`] and yields ready-to-send
//! [`PlannedBatch`]es, leaving the durable cursor to the caller.

use std::collections::BTreeMap;

use edge_tsdb::{SeriesId, Tier, TsdbError};
use mesh_protocol::{BackfillSample, BackfillTier};

use super::{
    roll_to_60s, BackfillConfig, BackfillCursors, BucketReduction, TierReader, HOUR_STEP, MIN_STEP,
    RECENT_STEP,
};
use crate::ml::store_sink::{
    series_dim_name, series_max_dim_name, series_reduction, WindowReduction,
};

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
            Phase::Recent => Some(BackfillTier::Recent60s),
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

    /// Samples one bucket produces across the active series: each series' average
    /// plus its maximum where it has one. The cap counts samples, so the bucket
    /// budget divides by this rather than by the series count.
    fn samples_per_bucket(&self) -> usize {
        self.series
            .iter()
            .map(|&series| {
                usize::from(series_dim_name(series).is_some())
                    + usize::from(series_max_dim_name(series).is_some())
            })
            .sum::<usize>()
            .max(1)
    }

    /// Buckets per batch = the sample cap spread across a bucket's samples (>=1).
    fn buckets_per_batch(&self) -> i64 {
        let per = self.cfg.max_batch_samples / self.samples_per_bucket();
        per.max(1) as i64
    }

    /// The `[lo, hi]` timestamp band and bucket step for a phase.
    fn band(&self, phase: Phase) -> (i64, i64, i64) {
        match phase {
            Phase::Recent => (
                self.now - self.cfg.recent_secs,
                self.now + self.cfg.future_skew_secs,
                RECENT_STEP,
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
            // An empty interval: nothing can be at once above the largest
            // timestamp and below the smallest, which is what a finished walk
            // has left to cover.
            Phase::Done => (i64::MAX, i64::MIN, RECENT_STEP),
        }
    }

    /// Whether a bucket `[ts, ts+step)` may ship in the current phase. Each
    /// phase is bounded by its own band and by nothing else: the recent band's
    /// ceiling is the clock-skew allowance, which is what keeps a wild future
    /// reading out, and the older bands end well inside the past.
    ///
    /// A rollup bucket ships only if it lies **entirely** inside its tier's time
    /// range, so a coarse bucket that straddles into a finer tier's range is
    /// dropped rather than double-counting the same wall-clock time at two
    /// resolutions. The recent tier has no such straddle to guard — its buckets
    /// are the finest resolution shipped — so its own ceiling bounds it.
    fn emit_ok(&self, ts: i64, step: i64) -> bool {
        let (lo, hi, _) = self.band(self.phase);
        match self.phase {
            Phase::Recent => ts >= lo && ts <= hi,
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
                for (series, avg, max) in dims {
                    if let Some(name) = series_dim_name(series) {
                        samples.push(BackfillSample {
                            name: name.to_string(),
                            ts,
                            value: avg,
                        });
                    }
                    if let Some(name) = series_max_dim_name(series) {
                        samples.push(BackfillSample {
                            name: name.to_string(),
                            ts,
                            value: max,
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
    /// bucket-ts → the `(series, avg, max)` triples at that bucket, capped to
    /// `buckets_per_batch` distinct buckets and bounded by [`emit_ok`].
    ///
    /// The rollup tiers read `max` from the stored bucket rather than deriving it
    /// from the averages, so a bucket's maximum is the largest raw sample in it
    /// and not the largest of its own means.
    ///
    /// A stall vital publishes its latest reading wherever the bucket is the
    /// 60 s the kernel itself averaged over — the recent tier and the 1 min
    /// rollup. The 1 hr rollup spans sixty of those kernel windows, so its mean
    /// summarizes the hour the way it does for every other series.
    ///
    /// [`emit_ok`]: BackfillDrain::emit_ok
    fn read_buckets(
        &self,
        tier: BackfillTier,
        step: i64,
        start: i64,
        end: i64,
    ) -> Result<BTreeMap<i64, Vec<BucketReduction>>, TsdbError> {
        let mut acc: BTreeMap<i64, Vec<BucketReduction>> = BTreeMap::new();
        for &series in self.series {
            let reduction = series_reduction(series);
            let points: Vec<(i64, f64, f64)> = match tier {
                BackfillTier::Recent60s => {
                    roll_to_60s(&self.reader.range_raw(series, start, end)?, reduction)
                }
                BackfillTier::Rollup1m => self
                    .reader
                    .range_tier(series, Tier::T1, start, end)?
                    .into_iter()
                    .map(|p| {
                        let value = match reduction {
                            WindowReduction::Mean => p.avg,
                            WindowReduction::Last => p.last,
                        };
                        (p.bucket, value, p.max)
                    })
                    .collect(),
                BackfillTier::Rollup1h => self
                    .reader
                    .range_tier(series, Tier::T2, start, end)?
                    .into_iter()
                    .map(|p| (p.bucket, p.avg, p.max))
                    .collect(),
                _ => Vec::new(),
            };
            for (ts, avg, max) in points {
                if self.emit_ok(ts, step) {
                    acc.entry(ts).or_default().push((series, avg, max));
                }
            }
        }
        // Cap to buckets_per_batch distinct bucket timestamps (ascending). The
        // first bucket past the cap is where the map is cut, so an uncapped read
        // walks no further than one key past what it keeps.
        let cap = self.buckets_per_batch() as usize;
        if let Some(&first_dropped) = acc.keys().nth(cap) {
            acc.split_off(&first_dropped);
        }
        Ok(acc)
    }
}
