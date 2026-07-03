package correlate

import (
	"math"
	"sort"
)

// anomalyStdDevs is the band half-width (in baseline standard deviations) beyond
// which a focus-window sample counts as anomalous.
const anomalyStdDevs = 3.0

// ksStatistic returns the two-sample Kolmogorov–Smirnov statistic D — the
// maximum absolute difference between the two empirical CDFs — in [0, 1]. D is 0
// for identical samples and 1 for fully separated ones. The statistic is
// symmetric in its arguments. An empty sample yields 0 (no distribution to
// compare).
func ksStatistic(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	as := append([]float64(nil), a...)
	bs := append([]float64(nil), b...)
	sort.Float64s(as)
	sort.Float64s(bs)

	na, nb := float64(len(as)), float64(len(bs))
	var i, j int
	var d float64
	for i < len(as) && j < len(bs) {
		x := math.Min(as[i], bs[j])
		for i < len(as) && as[i] <= x {
			i++
		}
		for j < len(bs) && bs[j] <= x {
			j++
		}
		if diff := math.Abs(float64(i)/na - float64(j)/nb); diff > d {
			d = diff
		}
	}
	return d
}

// anomalyRate returns the fraction of focus-window samples that fall outside the
// baseline's mean ± anomalyStdDevs·stddev band, in [0, 1]. A degenerate
// (zero-variance) baseline treats any focus value differing from the baseline
// mean as anomalous. An empty focus window yields 0.
func anomalyRate(baseline, focus []float64) float64 {
	if len(focus) == 0 {
		return 0
	}
	mean, std := meanStdDev(baseline)
	band := anomalyStdDevs * std
	var anomalous int
	for _, v := range focus {
		if std == 0 {
			if v != mean {
				anomalous++
			}
			continue
		}
		if math.Abs(v-mean) > band {
			anomalous++
		}
	}
	return float64(anomalous) / float64(len(focus))
}

// shiftMagnitude returns the size of the mean shift from baseline to focus,
// normalized by the baseline's own scale (|mean| + stddev), clamped to [0, 1].
// KS and anomaly-rate saturate on any fully separated shift regardless of size;
// this "volume" term distinguishes a large regression from a negligible drift.
func shiftMagnitude(baseline, focus []float64) float64 {
	if len(baseline) == 0 || len(focus) == 0 {
		return 0
	}
	bMean, bStd := meanStdDev(baseline)
	fMean, _ := meanStdDev(focus)
	scale := math.Abs(bMean) + bStd
	if scale == 0 {
		// Degenerate all-zero baseline: any nonzero focus mean is a full shift.
		if fMean == 0 {
			return 0
		}
		return 1
	}
	rel := math.Abs(fMean-bMean) / scale
	if rel > 1 {
		return 1
	}
	return rel
}

// meanStdDev returns the population mean and standard deviation of xs. An empty
// slice yields (0, 0).
func meanStdDev(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range xs {
		sum += v
	}
	mean = sum / float64(len(xs))
	var sq float64
	for _, v := range xs {
		d := v - mean
		sq += d * d
	}
	return mean, math.Sqrt(sq / float64(len(xs)))
}
