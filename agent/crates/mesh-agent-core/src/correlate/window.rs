//! The two windows a correlation compares, and the bounded read that fills them
//! from the agent's own store.

use edge_tsdb::store::TsdbSnapshot;
use edge_tsdb::TsdbError;

use crate::ml::store_sink::{series_dim_name, BACKFILL_SERIES};

use super::rank::{rank_dimensions, CorrelationLimits, DimWindows, Ranking};

/// A baseline window and the focus window it is compared against, in whole Unix
/// seconds — the timestamps the sampler writes.
///
/// The two meet without overlapping: the baseline runs up to but not including
/// its end, and the focus includes the instant it ends on. A reading on the
/// boundary therefore belongs to the focus, which is the window an alert is
/// about.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CorrelationWindow {
    baseline_start: i64,
    baseline_end: i64,
    focus_start: i64,
    focus_end: i64,
}

impl CorrelationWindow {
    /// Build a window from explicit bounds, or `None` when either half does not
    /// run forwards.
    #[must_use]
    pub fn new(
        baseline_start: i64,
        baseline_end: i64,
        focus_start: i64,
        focus_end: i64,
    ) -> Option<Self> {
        if baseline_start >= baseline_end || focus_start >= focus_end {
            return None;
        }
        Some(Self {
            baseline_start,
            baseline_end,
            focus_start,
            focus_end,
        })
    }

    /// Build a window whose baseline is the stretch of equal length immediately
    /// before the focus — what a rule firing over an interval has to compare
    /// against when no other baseline is named.
    #[must_use]
    pub fn preceding_baseline(focus_start: i64, focus_end: i64) -> Option<Self> {
        let width = focus_end.checked_sub(focus_start)?;
        Self::new(
            focus_start.checked_sub(width)?,
            focus_start,
            focus_start,
            focus_end,
        )
    }

    /// First instant of the baseline window.
    #[must_use]
    pub fn baseline_start(&self) -> i64 {
        self.baseline_start
    }

    /// The instant the baseline window runs up to, exclusive.
    #[must_use]
    pub fn baseline_end(&self) -> i64 {
        self.baseline_end
    }

    /// First instant of the focus window.
    #[must_use]
    pub fn focus_start(&self) -> i64 {
        self.focus_start
    }

    /// Last instant of the focus window, inclusive.
    #[must_use]
    pub fn focus_end(&self) -> i64 {
        self.focus_end
    }

    /// Split `points` into `(baseline, focus)` readings, keeping at most
    /// `max_points` in each.
    ///
    /// A reading that is not a real number is dropped here — the one place it
    /// can enter — so nothing downstream has to defend against a NaN.
    #[must_use]
    pub fn split(&self, points: &[(i64, f64)], max_points: usize) -> (Vec<f64>, Vec<f64>) {
        let mut baseline = Vec::new();
        let mut focus = Vec::new();
        for &(ts, value) in points {
            if !value.is_finite() {
                continue;
            }
            if (self.baseline_start..self.baseline_end).contains(&ts) {
                if baseline.len() < max_points {
                    baseline.push(value);
                }
            } else if (self.focus_start..=self.focus_end).contains(&ts) && focus.len() < max_points
            {
                focus.push(value);
            }
        }
        (baseline, focus)
    }
}

/// Rank the host dimensions the local store holds for `window`, reading through
/// an MVCC `snapshot` so the sampler can keep writing underneath.
///
/// The read is bounded before the scoring is: each series is fetched over the
/// span the two windows cover and no further, at most
/// [`CorrelationLimits::max_dims`] series are touched, and each window keeps at
/// most [`CorrelationLimits::max_points_per_window`] readings.
pub fn correlate_snapshot(
    snapshot: &TsdbSnapshot,
    window: &CorrelationWindow,
    limits: &CorrelationLimits,
) -> Result<Ranking, TsdbError> {
    // The store's range read is half-open; the focus window includes its end.
    let read_end = window.focus_end().saturating_add(1);

    let mut split = Vec::with_capacity(BACKFILL_SERIES.len());
    for series in BACKFILL_SERIES.into_iter().take(limits.max_dims) {
        let Some(dim) = series_dim_name(series) else {
            continue;
        };
        let points: Vec<(i64, f64)> = snapshot
            .range_raw(series, window.baseline_start(), read_end)?
            .into_iter()
            .map(|(sample, _anomaly)| (sample.ts, sample.value))
            .collect();
        let (baseline, focus) = window.split(&points, limits.max_points_per_window);
        split.push((dim, baseline, focus));
    }

    let dims: Vec<DimWindows<'_>> = split
        .iter()
        .map(|(dim, baseline, focus)| DimWindows {
            dim,
            baseline,
            focus,
        })
        .collect();
    Ok(rank_dimensions(&dims, limits))
}

#[cfg(test)]
mod tests {
    use super::CorrelationWindow;

    /// The point cap applies to each window on its own, and keeps the earliest
    /// readings — the ones that establish what the window looked like.
    #[test]
    fn each_window_keeps_at_most_its_cap() {
        let window = CorrelationWindow::new(0, 10, 10, 20).expect("window");
        let points: Vec<(i64, f64)> = (0..=20i32)
            .map(|ts| (i64::from(ts), f64::from(ts)))
            .collect();
        let (baseline, focus) = window.split(&points, 3);
        assert_eq!(baseline, vec![0.0, 1.0, 2.0]);
        assert_eq!(focus, vec![10.0, 11.0, 12.0]);
    }

    /// A reading outside both windows belongs to neither.
    #[test]
    fn readings_outside_both_windows_are_dropped() {
        let window = CorrelationWindow::new(100, 200, 200, 300).expect("window");
        let (baseline, focus) = window.split(&[(50, 1.0), (400, 2.0)], 10);
        assert!(baseline.is_empty());
        assert!(focus.is_empty());
    }

    /// A focus window that would need a baseline before the epoch's negative
    /// bound is refused rather than wrapping.
    #[test]
    fn a_baseline_that_cannot_be_computed_is_refused() {
        assert!(CorrelationWindow::preceding_baseline(i64::MIN, 0).is_none());
        assert!(CorrelationWindow::preceding_baseline(i64::MIN + 1, i64::MAX).is_none());
    }
}
