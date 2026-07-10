//! Tier rollups: the min/max/last (+avg) the edge store owns because central VM
//! keeps only `avg`.
//!
//! T0 is the raw 1 s series held by the substrate. T1 and T2 are deterministic
//! reductions over fixed time buckets (60 s, 3600 s), keyed by *sample*
//! timestamp — never arrival wall-clock — so an NTP step cannot move a point
//! into the wrong bucket. Rollups are a pure function of the raw series, which
//! is why they can be recomputed after a crash instead of being separately
//! crash-protected.

use crate::sample::Sample;

/// One aggregated bucket: the four statistics the offline store must serve that
/// central `avg`-only VM cannot.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct TierPoint {
    /// Bucket start (Unix seconds, floored to the interval).
    pub bucket: i64,
    /// Minimum raw value in the bucket.
    pub min: f64,
    /// Maximum raw value in the bucket.
    pub max: f64,
    /// Mean raw value in the bucket.
    pub avg: f64,
    /// Last raw value (largest timestamp) in the bucket.
    pub last: f64,
    /// Number of raw samples folded into the bucket.
    pub count: u32,
}

/// Common rollup intervals.
pub const T1_MINUTE: i64 = 60;
/// Coarse (hourly) rollup interval.
pub const T2_HOUR: i64 = 3600;

/// Reduce a raw series into `interval`-second buckets. Input need not be sorted
/// or monotonic; buckets are assigned by `floor(ts / interval)`.
#[must_use]
pub fn rollup(samples: &[Sample], interval: i64) -> Vec<TierPoint> {
    assert!(interval > 0, "interval must be positive");
    // Accumulate per bucket, preserving first-seen bucket order deterministically
    // by sorting keys at the end.
    use std::collections::BTreeMap;
    struct Acc {
        min: f64,
        max: f64,
        sum: f64,
        last: f64,
        last_ts: i64,
        count: u32,
    }
    let mut acc: BTreeMap<i64, Acc> = BTreeMap::new();
    for s in samples {
        let bucket = s.ts.div_euclid(interval) * interval;
        let e = acc.entry(bucket).or_insert(Acc {
            min: s.value,
            max: s.value,
            sum: 0.0,
            last: s.value,
            last_ts: i64::MIN,
            count: 0,
        });
        e.min = e.min.min(s.value);
        e.max = e.max.max(s.value);
        e.sum += s.value;
        e.count += 1;
        if s.ts >= e.last_ts {
            e.last_ts = s.ts;
            e.last = s.value;
        }
    }
    acc.into_iter()
        .map(|(bucket, a)| TierPoint {
            bucket,
            min: a.min,
            max: a.max,
            avg: a.sum / f64::from(a.count),
            last: a.last,
            count: a.count,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::{rollup, T1_MINUTE};
    use crate::sample::Sample;

    #[test]
    fn folds_minute_buckets_with_true_extrema() {
        let mut samples = Vec::new();
        // Minute 0: values 0..60, so min 0, max 59, last 59.
        for i in 0..60 {
            samples.push(Sample::new(i, i as f64));
        }
        // Minute 1: a single spike.
        samples.push(Sample::new(60, 1000.0));
        let points = rollup(&samples, T1_MINUTE);
        assert_eq!(points.len(), 2);
        assert_eq!(points[0].bucket, 0);
        assert_eq!(points[0].min, 0.0);
        assert_eq!(points[0].max, 59.0);
        assert_eq!(points[0].last, 59.0);
        assert_eq!(points[0].count, 60);
        assert!((points[0].avg - 29.5).abs() < 1e-9);
        assert_eq!(points[1].bucket, 60);
        assert_eq!(points[1].max, 1000.0);
    }

    #[test]
    fn bucketing_keys_on_sample_ts_not_arrival_order() {
        // Out-of-order arrival (NTP step): later-arriving older sample still
        // lands in its own bucket and does not become "last".
        let samples = vec![
            Sample::new(90, 5.0), // minute 1
            Sample::new(10, 1.0), // minute 0, arrives second
            Sample::new(20, 2.0), // minute 0
        ];
        let points = rollup(&samples, T1_MINUTE);
        assert_eq!(points.len(), 2);
        assert_eq!(points[0].bucket, 0);
        assert_eq!(points[0].last, 2.0); // ts=20 is the latest in minute 0
        assert_eq!(points[1].bucket, 60);
        assert_eq!(points[1].last, 5.0);
    }
}
