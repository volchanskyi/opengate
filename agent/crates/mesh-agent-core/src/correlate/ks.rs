//! The three co-signals a dimension is scored on.
//!
//! Each is a number in `[0, 1]`, each is defined for every input a real store
//! can hand it — including a window with no readings, one reading, or a flat
//! run of the same reading — and none of them can return a NaN. That property
//! is load-bearing rather than incidental: a score that came out NaN would sort
//! unpredictably and put a meaningless dimension at the top of an alert an
//! investigator is trusting.

/// The band half-width, in baseline standard deviations, beyond which a focus
/// reading counts as anomalous.
const ANOMALY_STD_DEVS: f64 = 3.0;

/// The two-sample Kolmogorov–Smirnov statistic *D*: the largest gap between the
/// two windows' empirical distributions, in `[0, 1]`. It is 0 for identical
/// windows and 1 for windows that do not overlap at all, and it is symmetric in
/// its arguments. A window with no readings yields 0 — there is no distribution
/// to compare against.
///
/// This is what answers "did this dimension *change shape*", as opposed to "did
/// its average move": a CPU that spent the baseline flat at 20 % and the focus
/// alternating between 0 and 40 has the same mean and a D of 1.
#[must_use]
pub fn ks_statistic(a: &[f64], b: &[f64]) -> f64 {
    if a.is_empty() || b.is_empty() {
        return 0.0;
    }
    let mut sorted_a = a.to_vec();
    let mut sorted_b = b.to_vec();
    sorted_a.sort_by(|x, y| x.total_cmp(y));
    sorted_b.sort_by(|x, y| x.total_cmp(y));

    let na = sorted_a.len() as f64;
    let nb = sorted_b.len() as f64;
    let (mut i, mut j) = (0usize, 0usize);
    let mut d = 0.0;
    while i < sorted_a.len() && j < sorted_b.len() {
        let x = sorted_a[i].min(sorted_b[j]);
        while i < sorted_a.len() && sorted_a[i] <= x {
            i += 1;
        }
        while j < sorted_b.len() && sorted_b[j] <= x {
            j += 1;
        }
        let diff = (i as f64 / na - j as f64 / nb).abs();
        if diff > d {
            d = diff;
        }
    }
    d
}

/// The share of focus readings that fall outside the baseline's mean ±
/// [`ANOMALY_STD_DEVS`] standard deviations, in `[0, 1]`.
///
/// A baseline with no spread at all — a gauge that read the same number all
/// hour — has no band, so any different reading counts. An empty focus window
/// yields 0.
#[must_use]
pub fn anomaly_rate(baseline: &[f64], focus: &[f64]) -> f64 {
    if focus.is_empty() {
        return 0.0;
    }
    let (mean, std) = mean_std_dev(baseline);
    let band = ANOMALY_STD_DEVS * std;
    let anomalous = focus
        .iter()
        .filter(|&&v| {
            if std == 0.0 {
                v != mean
            } else {
                (v - mean).abs() > band
            }
        })
        .count();
    anomalous as f64 / focus.len() as f64
}

/// How far the mean moved from baseline to focus, measured against the
/// baseline's own scale (`|mean| + stddev`) and clamped to `[0, 1]`.
///
/// The other two signals saturate on any clean separation regardless of its
/// size: a service time that went from 0.40 ms to 0.44 ms scores the same D as
/// one that went from 0.4 ms to 40 ms. This term is what separates the
/// regression a technician has to act on from a drift nobody would notice. A
/// baseline that is all zeroes has no scale, so any nonzero focus mean is a
/// complete shift.
#[must_use]
pub fn shift_magnitude(baseline: &[f64], focus: &[f64]) -> f64 {
    if baseline.is_empty() || focus.is_empty() {
        return 0.0;
    }
    let (baseline_mean, baseline_std) = mean_std_dev(baseline);
    let (focus_mean, _) = mean_std_dev(focus);
    let scale = baseline_mean.abs() + baseline_std;
    if scale == 0.0 {
        return if focus_mean == 0.0 { 0.0 } else { 1.0 };
    }
    ((focus_mean - baseline_mean).abs() / scale).min(1.0)
}

/// The population mean and standard deviation of `xs`; `(0, 0)` for an empty
/// window.
#[must_use]
pub fn mean_std_dev(xs: &[f64]) -> (f64, f64) {
    if xs.is_empty() {
        return (0.0, 0.0);
    }
    let n = xs.len() as f64;
    let mean = xs.iter().sum::<f64>() / n;
    let variance = xs.iter().map(|v| (v - mean) * (v - mean)).sum::<f64>() / n;
    (mean, variance.sqrt())
}

#[cfg(test)]
mod tests {
    use super::{anomaly_rate, ks_statistic, mean_std_dev, shift_magnitude};

    #[test]
    fn the_statistic_reads_the_gap_between_two_distributions() {
        // Half of one window sits below all of the other: D = 0.5.
        assert_eq!(ks_statistic(&[0.0, 0.0, 1.0, 1.0], &[1.0, 1.0]), 0.5);
    }

    #[test]
    fn an_empty_window_compares_to_nothing() {
        assert_eq!(ks_statistic(&[], &[1.0]), 0.0);
        assert_eq!(ks_statistic(&[1.0], &[]), 0.0);
        assert_eq!(anomaly_rate(&[1.0], &[]), 0.0);
        assert_eq!(shift_magnitude(&[], &[1.0]), 0.0);
        assert_eq!(shift_magnitude(&[1.0], &[]), 0.0);
        assert_eq!(mean_std_dev(&[]), (0.0, 0.0));
    }

    #[test]
    fn a_reading_inside_the_band_is_not_anomalous() {
        // Baseline mean 3, stddev ~1.41: the band reaches past 7.
        let baseline = [1.0, 3.0, 5.0];
        assert_eq!(anomaly_rate(&baseline, &[3.0, 4.0]), 0.0);
        assert_eq!(anomaly_rate(&baseline, &[3.0, 100.0]), 0.5);
    }

    #[test]
    fn the_shift_is_measured_against_the_baselines_own_scale() {
        // A move of 1 against a baseline sitting at 10 with no spread.
        assert_eq!(shift_magnitude(&[10.0, 10.0], &[11.0, 11.0]), 0.1);
        // A move larger than the scale itself saturates rather than exceeding 1.
        assert_eq!(shift_magnitude(&[10.0, 10.0], &[40.0, 40.0]), 1.0);
    }

    #[test]
    fn the_mean_and_spread_are_the_population_form() {
        assert_eq!(mean_std_dev(&[2.0, 4.0]), (3.0, 1.0));
    }
}
