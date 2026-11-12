//! The age bands and the bucket reads a drain walks: which buckets each band may send, and how one
//! slice of a tier is read and named.

use std::collections::BTreeMap;

use edge_tsdb::{Tier, TsdbError};
use mesh_protocol::{BackfillSample, BackfillTier};

use super::super::{roll_to_60s, BucketReduction, TierReader, HOUR_STEP, MIN_STEP, RECENT_STEP};
use super::{BackfillDrain, Phase};
use crate::ml::store_sink::{
    series_dim_name, series_max_dim_name, series_reduction, WindowReduction,
};

/// A slice's buckets, each with its `(series, avg, max)` triples, ascending.
pub(super) type Buckets = BTreeMap<i64, Vec<BucketReduction>>;

impl<R: TierReader> BackfillDrain<'_, R> {
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

    pub(super) fn buckets_per_batch(&self) -> i64 {
        let per = self.cfg.max_batch_samples / self.samples_per_bucket();
        per.max(1) as i64
    }

    pub(super) fn band(&self, phase: Phase) -> (i64, i64, i64) {
        match phase {
            // The newest bucket is the last whole minute: a minute still running is never sent.
            Phase::Recent => (
                self.now - self.cfg.recent_secs,
                self.now - RECENT_STEP,
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

    /// The time the band's buckets end by: the newest band's last window ends at `now`, a
    /// rollup band's buckets at the band's top.
    pub(super) fn band_end(&self, phase: Phase) -> i64 {
        let (_, hi, step) = self.band(phase);
        match phase {
            Phase::Recent => hi + step,
            Phase::Mid | Phase::Old | Phase::Done => hi,
        }
    }

    /// Whether a bucket may ship in the current phase: ended, unconfirmed, and for a rollup
    /// entirely in its band so no time ships at two resolutions.
    fn emit_ok(&self, ts: i64, step: i64) -> bool {
        let (lo, hi, _) = self.band(self.at.phase);
        let in_band = match self.at.phase {
            Phase::Recent => ts >= lo && ts <= hi,
            Phase::Mid | Phase::Old => ts >= lo && ts + step <= hi,
            Phase::Done => false,
        };
        in_band && ts + step <= self.now && !self.confirmed.covers(ts, ts + step)
    }

    /// Reads the buckets from `start` through `end` for `tier` into `(series, avg, max)` triples,
    /// capped to `buckets_per_batch`, and flags an unconfirmed point the band cannot send.
    pub(super) fn read_buckets(
        &self,
        tier: BackfillTier,
        step: i64,
        start: i64,
        end: i64,
    ) -> Result<(Buckets, bool), TsdbError> {
        let mut acc = Buckets::new();
        let mut owed_elsewhere = false;
        for &series in self.series {
            let reduction = series_reduction(series);
            let points: Vec<(i64, f64, f64)> = match tier {
                // A 60 s window starting at `end` holds readings up to a minute past it.
                BackfillTier::Recent60s => roll_to_60s(
                    &self.reader.range_raw(series, start, end + RECENT_STEP)?,
                    reduction,
                ),
                BackfillTier::Rollup1m => self
                    .reader
                    .range_tier(series, Tier::T1, start, end + 1)?
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
                    .range_tier(series, Tier::T2, start, end + 1)?
                    .into_iter()
                    .map(|p| (p.bucket, p.avg, p.max))
                    .collect(),
                _ => Vec::new(),
            };
            for (ts, avg, max) in points {
                if self.emit_ok(ts, step) {
                    acc.entry(ts).or_default().push((series, avg, max));
                } else if !self.confirmed.covers(ts, ts + step) {
                    owed_elsewhere = true;
                }
            }
        }
        // The map is cut at the first bucket past the cap.
        let cap = self.buckets_per_batch() as usize;
        if let Some(&first_dropped) = acc.keys().nth(cap) {
            acc.split_off(&first_dropped);
        }
        Ok((acc, owed_elsewhere))
    }
}

/// Names each bucket's average and maximum as the dimensions the server stores.
pub(super) fn named_samples(buckets: Buckets) -> Vec<BackfillSample> {
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
    samples
}
