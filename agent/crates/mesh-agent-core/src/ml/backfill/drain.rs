//! The tier walk: recent-first, then oldest-first per tier from a durable watermark, so an
//! interrupted replay resumes without re-sending or shipping one time at two resolutions.

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

/// A batch of pre-rolled samples for one tier, ascending by timestamp, ending at bucket `cursor`.
#[derive(Debug, Clone, PartialEq)]
pub struct PlannedBatch {
    pub tier: BackfillTier,
    pub samples: Vec<BackfillSample>,
    pub cursor: i64,
}

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

/// A recent-first drain over a [`TierReader`]; `next_batch` yields batches until `None`.
pub struct BackfillDrain<'a, R: TierReader> {
    reader: &'a R,
    now: i64,
    cfg: BackfillConfig,
    series: &'a [SeriesId],
    cursors: BackfillCursors,
    phase: Phase,
    pos: i64,
    pos_ready: bool,
}

impl<'a, R: TierReader> BackfillDrain<'a, R> {
    /// Starts a drain from the given durable cursors.
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

    /// Samples one bucket yields across the series: an average plus a maximum where one exists.
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

    fn buckets_per_batch(&self) -> i64 {
        let per = self.cfg.max_batch_samples / self.samples_per_bucket();
        per.max(1) as i64
    }

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
            // An empty interval: no timestamp is both above `i64::MAX` and below `i64::MIN`.
            Phase::Done => (i64::MAX, i64::MIN, RECENT_STEP),
        }
    }

    /// Whether a bucket may ship in the current phase; a rollup bucket must lie entirely in its
    /// band so no time ships at two resolutions.
    fn emit_ok(&self, ts: i64, step: i64) -> bool {
        let (lo, hi, _) = self.band(self.phase);
        match self.phase {
            Phase::Recent => ts >= lo && ts <= hi,
            Phase::Mid | Phase::Old => ts >= lo && ts + step <= hi,
            Phase::Done => false,
        }
    }

    /// Produces the next batch, or `None` when drained; the caller owns the durable cursor.
    pub fn next_batch(&mut self) -> Result<Option<PlannedBatch>, TsdbError> {
        loop {
            let Some(tier) = self.phase.tier() else {
                return Ok(None);
            };
            let (band_lo, band_hi, step) = self.band(self.phase);

            if !self.pos_ready {
                // Resumes strictly after the watermark and never before the band floor.
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
                // An evicted or empty slice is skipped past the scanned window.
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

    /// Reads `[start, end]` for `tier` into per-bucket `(series, avg, max)` triples, capped to
    /// `buckets_per_batch` buckets; rollup tiers take `max` from the stored bucket.
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
        // The map is cut at the first bucket past the cap.
        let cap = self.buckets_per_batch() as usize;
        if let Some(&first_dropped) = acc.keys().nth(cap) {
            acc.split_off(&first_dropped);
        }
        Ok(acc)
    }
}
