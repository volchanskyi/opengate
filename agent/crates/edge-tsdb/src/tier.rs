//! Tier rollups (min/max/last/avg) over fixed 60 s and 3600 s buckets, keyed by sample
//! timestamp so an NTP step cannot move a point into the wrong bucket.

use crate::error::{Result, TsdbError};
use crate::sample::Sample;

/// One aggregated bucket: min, max, avg and last over the raw samples it holds.
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

/// The persisted rollup bucket; `sum` and `last_ts` let partial rollups of one bucket merge
/// exactly and in any order.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct StoredTierPoint {
    /// Bucket start (Unix seconds, floored to the interval).
    pub bucket: i64,
    /// Minimum raw value in the bucket.
    pub min: f64,
    /// Maximum raw value in the bucket.
    pub max: f64,
    /// Sum of raw values; `avg` derives from it at read time.
    pub sum: f64,
    /// Last raw value (the one with the largest timestamp).
    pub last: f64,
    /// Timestamp of `last`, so a merge picks the truly-latest value.
    pub last_ts: i64,
    /// Number of raw samples folded into the bucket.
    pub count: u32,
}

impl StoredTierPoint {
    /// Folds a partial rollup of the same bucket into `self`; commutative and associative.
    pub fn merge(&mut self, other: &StoredTierPoint) {
        debug_assert_eq!(self.bucket, other.bucket);
        self.min = self.min.min(other.min);
        self.max = self.max.max(other.max);
        self.sum += other.sum;
        self.count += other.count;
        if other.last_ts >= self.last_ts {
            self.last_ts = other.last_ts;
            self.last = other.last;
        }
    }

    /// Project to the public [`TierPoint`] (computing `avg = sum / count`).
    #[must_use]
    pub fn to_public(&self) -> TierPoint {
        TierPoint {
            bucket: self.bucket,
            min: self.min,
            max: self.max,
            avg: if self.count == 0 {
                0.0
            } else {
                self.sum / f64::from(self.count)
            },
            last: self.last,
            count: self.count,
        }
    }
}

/// Common rollup intervals.
pub const T1_MINUTE: i64 = 60;
/// Coarse (hourly) rollup interval.
pub const T2_HOUR: i64 = 3600;

/// Reduces a raw series into [`StoredTierPoint`] buckets of `interval` seconds, in any order.
#[must_use]
pub fn stored_rollup(samples: &[Sample], interval: i64) -> Vec<StoredTierPoint> {
    assert!(interval > 0, "interval must be positive");
    use std::collections::BTreeMap;
    let mut acc: BTreeMap<i64, StoredTierPoint> = BTreeMap::new();
    for s in samples {
        let bucket = s.ts.div_euclid(interval) * interval;
        let e = acc.entry(bucket).or_insert(StoredTierPoint {
            bucket,
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
    acc.into_values().collect()
}

/// Encodes stored rollup points as a struct-of-arrays block, which DEFLATE compresses well.
#[must_use]
pub fn encode_tier_block(points: &[StoredTierPoint]) -> Vec<u8> {
    let mut out = Vec::with_capacity(4 + points.len() * 52);
    out.extend_from_slice(&(points.len() as u32).to_le_bytes());
    for p in points {
        out.extend_from_slice(&p.bucket.to_le_bytes());
    }
    for p in points {
        out.extend_from_slice(&p.min.to_le_bytes());
    }
    for p in points {
        out.extend_from_slice(&p.max.to_le_bytes());
    }
    for p in points {
        out.extend_from_slice(&p.sum.to_le_bytes());
    }
    for p in points {
        out.extend_from_slice(&p.last.to_le_bytes());
    }
    for p in points {
        out.extend_from_slice(&p.last_ts.to_le_bytes());
    }
    for p in points {
        out.extend_from_slice(&p.count.to_le_bytes());
    }
    out
}

/// Decodes a block from [`encode_tier_block`]; a truncated or malformed block is an error.
pub fn decode_tier_block(bytes: &[u8]) -> Result<Vec<StoredTierPoint>> {
    let err = || TsdbError::CorruptBlock("tier block");
    if bytes.len() < 4 {
        return Err(err());
    }
    let count = u32::from_le_bytes(bytes[0..4].try_into().unwrap()) as usize;
    // 8 (bucket) + 8+8+8+8 (min/max/sum/last) + 8 (last_ts) + 4 (count) = 52 B.
    let needed = 4usize
        .checked_add(count.checked_mul(52).ok_or_else(err)?)
        .ok_or_else(err)?;
    if bytes.len() < needed {
        return Err(err());
    }
    let mut pos = 4usize;
    let take_i64 = |n: usize, pos: &mut usize| -> Vec<i64> {
        let v = (0..n)
            .map(|i| i64::from_le_bytes(bytes[*pos + i * 8..*pos + i * 8 + 8].try_into().unwrap()))
            .collect();
        *pos += n * 8;
        v
    };
    let buckets = take_i64(count, &mut pos);
    let take_f64 = |n: usize, pos: &mut usize| -> Vec<f64> {
        let v = (0..n)
            .map(|i| f64::from_le_bytes(bytes[*pos + i * 8..*pos + i * 8 + 8].try_into().unwrap()))
            .collect();
        *pos += n * 8;
        v
    };
    let mins = take_f64(count, &mut pos);
    let maxs = take_f64(count, &mut pos);
    let sums = take_f64(count, &mut pos);
    let lasts = take_f64(count, &mut pos);
    let last_tss = take_i64(count, &mut pos);
    let counts: Vec<u32> = (0..count)
        .map(|i| u32::from_le_bytes(bytes[pos + i * 4..pos + i * 4 + 4].try_into().unwrap()))
        .collect();
    Ok((0..count)
        .map(|i| StoredTierPoint {
            bucket: buckets[i],
            min: mins[i],
            max: maxs[i],
            sum: sums[i],
            last: lasts[i],
            last_ts: last_tss[i],
            count: counts[i],
        })
        .collect())
}

/// Reduces a raw series into `interval`-second buckets by `floor(ts / interval)`, in any order.
#[must_use]
pub fn rollup(samples: &[Sample], interval: i64) -> Vec<TierPoint> {
    stored_rollup(samples, interval)
        .into_iter()
        .map(|p| TierPoint {
            bucket: p.bucket,
            min: p.min,
            max: p.max,
            avg: p.sum / f64::from(p.count),
            last: p.last,
            count: p.count,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::{
        decode_tier_block, encode_tier_block, rollup, stored_rollup, StoredTierPoint, T1_MINUTE,
    };
    use crate::sample::Sample;

    #[test]
    fn stored_rollup_merge_is_order_independent() {
        let a: Vec<Sample> = (0..30).map(|i| Sample::new(i, i as f64)).collect();
        let b: Vec<Sample> = (30..60).map(|i| Sample::new(i, i as f64)).collect();
        let mut whole = stored_rollup(&a, T1_MINUTE);
        assert_eq!(whole.len(), 1);
        let mut second = stored_rollup(&b, T1_MINUTE);
        whole[0].merge(&second[0]);
        second[0].merge(&stored_rollup(&a, T1_MINUTE)[0]);
        assert_eq!(whole[0], second[0]);
        let p = whole[0].to_public();
        assert_eq!(p.min, 0.0);
        assert_eq!(p.max, 59.0);
        assert_eq!(p.last, 59.0);
        assert_eq!(p.count, 60);
        assert!((p.avg - 29.5).abs() < 1e-9);
    }

    #[test]
    fn tier_block_round_trips() {
        let points: Vec<StoredTierPoint> = (0..200)
            .map(|i| StoredTierPoint {
                bucket: i * 60,
                min: i as f64,
                max: i as f64 + 5.0,
                sum: i as f64 * 60.0,
                last: i as f64 + 2.0,
                last_ts: i * 60 + 59,
                count: 60,
            })
            .collect();
        let bytes = encode_tier_block(&points);
        assert_eq!(decode_tier_block(&bytes).unwrap(), points);
        assert!(decode_tier_block(&bytes[..bytes.len() / 2]).is_err());
        assert!(decode_tier_block(&[]).is_err());
    }

    #[test]
    fn folds_minute_buckets_with_true_extrema() {
        let mut samples = Vec::new();
        for i in 0..60 {
            samples.push(Sample::new(i, i as f64));
        }
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
        let samples = vec![
            Sample::new(90, 5.0),
            Sample::new(10, 1.0),
            Sample::new(20, 2.0),
        ];
        let points = rollup(&samples, T1_MINUTE);
        assert_eq!(points.len(), 2);
        assert_eq!(points[0].bucket, 0);
        assert_eq!(points[0].last, 2.0);
        assert_eq!(points[1].bucket, 60);
        assert_eq!(points[1].last, 5.0);
    }
}
