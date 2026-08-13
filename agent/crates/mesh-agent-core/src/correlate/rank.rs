//! The blend, the ordering, and the bounds.

use std::cmp::Ordering;
use std::time::{Duration, Instant};

use super::ks::{anomaly_rate, ks_statistic, shift_magnitude};

/// The fewest readings a window needs before a shift can be judged. One reading
/// has no distribution and no spread, so a dimension below this is left out of
/// the ranking rather than scored from nothing.
const MIN_WINDOW_SAMPLES: usize = 2;

/// How the three co-signals combine into the rank score. They sum to 1, so a
/// score is itself in `[0, 1]` and two dimensions are directly comparable.
/// These weights are **behaviour**, not tuning knobs: the frozen reference
/// ranking is scored against them.
const KS_WEIGHT: f64 = 0.4;
const ANOMALY_WEIGHT: f64 = 0.3;
const MAGNITUDE_WEIGHT: f64 = 0.3;

/// The default caps. A host writes thirteen dimensions at 1 Hz, so the
/// defaults leave both the dimension count and a quarter-hour focus window
/// comfortably inside their bound — the caps exist for the pathological case (a
/// rule firing repeatedly while the machine is already in trouble), not the
/// normal one.
const DEFAULT_TOP_N: usize = 8;
const DEFAULT_MAX_DIMS: usize = 32;
const DEFAULT_MAX_POINTS_PER_WINDOW: usize = 3_600;
const DEFAULT_BUDGET: Duration = Duration::from_millis(250);

/// One dimension's readings, already split into the two windows.
#[derive(Debug, Clone, Copy)]
pub struct DimWindows<'a> {
    /// The dimension's stable label, e.g. `disk.await_ms`.
    pub dim: &'a str,
    /// Readings from the baseline window.
    pub baseline: &'a [f64],
    /// Readings from the focus window.
    pub focus: &'a [f64],
}

/// One scored dimension.
#[derive(Debug, Clone, PartialEq)]
pub struct Ranked {
    /// The dimension's stable label.
    pub dim: String,
    /// The blended rank score in `[0, 1]`.
    pub score: f64,
    /// How much the distribution changed shape.
    pub ks_statistic: f64,
    /// The share of focus readings outside the baseline's band.
    pub anomaly_rate: f64,
    /// How far the mean moved, against the baseline's own scale.
    pub shift_magnitude: f64,
    /// Readings the baseline window contributed.
    pub baseline_samples: usize,
    /// Readings the focus window contributed.
    pub focus_samples: usize,
}

/// A completed ranking, with what it had to leave out.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Ranking {
    /// Scored dimensions, most anomalous first, capped at `top_n`.
    pub ranked: Vec<Ranked>,
    /// How many dimensions were examined, including ones too sparse to score.
    pub dims_considered: usize,
    /// Whether the dimension cap cut the candidate list short.
    pub dims_truncated: bool,
    /// Whether the time budget ran out before every candidate was scored.
    pub budget_exhausted: bool,
}

/// What a single correlation is allowed to cost.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CorrelationLimits {
    /// How many dimensions the ranking returns.
    pub top_n: usize,
    /// How many dimensions are examined at all.
    pub max_dims: usize,
    /// How many readings each window carries into the scoring.
    pub max_points_per_window: usize,
    /// The wall-clock the whole ranking may take. Checked between dimensions,
    /// so the first one is always scored and the run always terminates.
    pub budget: Duration,
}

impl Default for CorrelationLimits {
    fn default() -> Self {
        Self {
            top_n: DEFAULT_TOP_N,
            max_dims: DEFAULT_MAX_DIMS,
            max_points_per_window: DEFAULT_MAX_POINTS_PER_WINDOW,
            budget: DEFAULT_BUDGET,
        }
    }
}

/// Score every dimension and order them by how badly each broke pattern.
///
/// Ordering is score descending, then the distribution-shift statistic
/// descending, then the label ascending — a total order, so two runs over the
/// same readings produce the same list in the same order.
#[must_use]
pub fn rank_dimensions(dims: &[DimWindows<'_>], limits: &CorrelationLimits) -> Ranking {
    let started = Instant::now();
    let mut out = Ranking {
        dims_truncated: dims.len() > limits.max_dims,
        ..Ranking::default()
    };

    let candidates = dims.iter().take(limits.max_dims);
    let mut ranked = Vec::with_capacity(limits.max_dims.min(dims.len()));
    for dim in candidates {
        if out.dims_considered > 0 && started.elapsed() >= limits.budget {
            out.budget_exhausted = true;
            break;
        }
        out.dims_considered += 1;
        if let Some(r) = score_dimension(dim) {
            ranked.push(r);
        }
    }

    ranked.sort_by(most_anomalous_first);
    ranked.truncate(limits.top_n);
    out.ranked = ranked;
    out
}

/// Score one dimension, or `None` when either window is too sparse to judge.
fn score_dimension(dim: &DimWindows<'_>) -> Option<Ranked> {
    if dim.baseline.len() < MIN_WINDOW_SAMPLES || dim.focus.len() < MIN_WINDOW_SAMPLES {
        return None;
    }
    let ks = ks_statistic(dim.baseline, dim.focus);
    let anomaly = anomaly_rate(dim.baseline, dim.focus);
    let magnitude = shift_magnitude(dim.baseline, dim.focus);
    Some(Ranked {
        dim: dim.dim.to_owned(),
        score: KS_WEIGHT * ks + ANOMALY_WEIGHT * clamp01(anomaly) + MAGNITUDE_WEIGHT * magnitude,
        ks_statistic: ks,
        anomaly_rate: anomaly,
        shift_magnitude: magnitude,
        baseline_samples: dim.baseline.len(),
        focus_samples: dim.focus.len(),
    })
}

/// Order two scored dimensions: worse first, ties broken by shape change and
/// then by label so the order is reproducible.
///
/// Every score is a finite number by construction (each co-signal is defined
/// for every input and non-finite readings never reach here), which is what
/// lets a partial comparison stand in for a total one.
fn most_anomalous_first(a: &Ranked, b: &Ranked) -> Ordering {
    b.score
        .partial_cmp(&a.score)
        .unwrap_or(Ordering::Equal)
        .then_with(|| {
            b.ks_statistic
                .partial_cmp(&a.ks_statistic)
                .unwrap_or(Ordering::Equal)
        })
        .then_with(|| a.dim.cmp(&b.dim))
}

fn clamp01(v: f64) -> f64 {
    v.clamp(0.0, 1.0)
}

#[cfg(test)]
mod tests {
    use super::{most_anomalous_first, rank_dimensions, CorrelationLimits, DimWindows, Ranked};

    /// Dimensions that score identically fall back to the label, so a ranking
    /// never depends on the order the store happened to return them in.
    #[test]
    fn an_exact_tie_is_broken_by_label() {
        let baseline = [1.0, 1.0, 1.0];
        let focus = [5.0, 5.0, 5.0];
        let dims = [
            DimWindows {
                dim: "net.tx_bps",
                baseline: &baseline,
                focus: &focus,
            },
            DimWindows {
                dim: "cpu.total",
                baseline: &baseline,
                focus: &focus,
            },
            DimWindows {
                dim: "mem.used_percent",
                baseline: &baseline,
                focus: &focus,
            },
        ];
        let ranking = rank_dimensions(&dims, &CorrelationLimits::default());
        let order: Vec<&str> = ranking.ranked.iter().map(|r| r.dim.as_str()).collect();
        assert_eq!(order, vec!["cpu.total", "mem.used_percent", "net.tx_bps"]);
    }

    /// When two dimensions blend to the same score, the one whose distribution
    /// changed shape more is read first, and only then does the label decide.
    #[test]
    fn a_score_tie_is_broken_by_the_shift_in_shape_before_the_label() {
        let scored = |dim: &str, score: f64, ks: f64| Ranked {
            dim: dim.to_owned(),
            score,
            ks_statistic: ks,
            anomaly_rate: 0.0,
            shift_magnitude: 0.0,
            baseline_samples: 2,
            focus_samples: 2,
        };
        let mut ranked = [
            scored("a.small_shape_change", 0.7, 0.2),
            scored("z.large_shape_change", 0.7, 0.9),
            scored("m.same_shape_change", 0.7, 0.9),
            scored("b.higher_score", 0.8, 0.1),
        ];
        ranked.sort_by(most_anomalous_first);
        let order: Vec<&str> = ranked.iter().map(|r| r.dim.as_str()).collect();
        assert_eq!(
            order,
            vec![
                "b.higher_score",
                "m.same_shape_change",
                "z.large_shape_change",
                "a.small_shape_change",
            ]
        );
    }

    /// The blend is the behaviour the frozen reference is scored against: a
    /// complete separation with a complete move is a score of exactly 1.
    #[test]
    fn a_total_break_scores_one_and_a_flat_dimension_scores_zero() {
        let flat = [4.0, 4.0, 4.0];
        let broken_baseline = [1.0, 1.0, 1.0];
        let broken_focus = [90.0, 90.0, 90.0];
        let dims = [
            DimWindows {
                dim: "flat",
                baseline: &flat,
                focus: &flat,
            },
            DimWindows {
                dim: "broken",
                baseline: &broken_baseline,
                focus: &broken_focus,
            },
        ];
        let ranking = rank_dimensions(&dims, &CorrelationLimits::default());
        assert_eq!(ranking.ranked[0].dim, "broken");
        assert_eq!(ranking.ranked[0].score, 1.0);
        assert_eq!(ranking.ranked[1].score, 0.0);
    }
}
