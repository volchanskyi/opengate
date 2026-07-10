//! Invariants for the tier rollups that back the min/max/avg/last store.

use edge_tsdb::sample::Sample;
use edge_tsdb::tier::{rollup, stored_rollup};

/// `rollup` is the avg-collapsed view of `stored_rollup`; the two must bucket
/// identically so the presentation and stored forms can never diverge.
#[test]
fn rollup_agrees_with_stored_rollup() {
    let samples: Vec<Sample> = (0..500)
        .map(|i| Sample::new(i, (i % 11) as f64 * 1.5 - 4.0))
        .collect();
    let interval = 60;
    let public = rollup(&samples, interval);
    let stored = stored_rollup(&samples, interval);
    assert_eq!(public.len(), stored.len());
    assert!(!public.is_empty());
    for (p, s) in public.iter().zip(stored.iter()) {
        assert_eq!(p.bucket, s.bucket);
        assert_eq!(p.min, s.min);
        assert_eq!(p.max, s.max);
        assert_eq!(p.last, s.last);
        assert_eq!(p.count, s.count);
        assert!((p.avg - s.sum / f64::from(s.count)).abs() < 1e-9);
    }
}

/// Unsorted input buckets the same as sorted input (both rollups floor by ts).
#[test]
fn rollup_is_order_independent() {
    let mut samples: Vec<Sample> = (0..300).map(|i| Sample::new(i, i as f64)).collect();
    let sorted = rollup(&samples, 60);
    samples.reverse();
    let reversed = rollup(&samples, 60);
    assert_eq!(sorted, reversed);
}
