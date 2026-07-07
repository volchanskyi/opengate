package correlate

import (
	"math"
	"testing"
	"time"
)

// This file pins the arithmetic and comparison boundaries of the pure scoring
// helpers (ks.go and the correlate.go package functions) that the mutation suite
// flags. Each assertion holds only for the original operator.

// meanStdDev must return the population mean and standard deviation. The data
// {2,4,4,4,5,5,7,9} has mean 5 and population stddev 2 (variance 32/8=4).
func TestMeanStdDevPopulationValues(t *testing.T) {
	t.Parallel()
	mean, std := meanStdDev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	if math.Abs(mean-5) > 1e-9 {
		t.Fatalf("mean = %v, want 5", mean)
	}
	if math.Abs(std-2) > 1e-9 {
		t.Fatalf("population stddev = %v, want 2", std)
	}
	if m, s := meanStdDev(nil); m != 0 || s != 0 {
		t.Fatalf("empty meanStdDev = (%v, %v), want (0, 0)", m, s)
	}
}

// anomalyRate's band must be anomalyStdDevs·stddev (a product). Baseline {7,13}
// has mean 10, stddev 3, so band = 3·3 = 9: a focus value 8 away is in-band, one
// exactly 9 away stays in-band (strict >), and one 11 away is out-of-band.
func TestAnomalyRateBandIsThreeSigmaProduct(t *testing.T) {
	t.Parallel()
	baseline := []float64{7, 13} // mean 10, stddev 3, band 9
	if got := anomalyRate(baseline, []float64{18}); got != 0 {
		t.Fatalf("in-band (dist 8) rate = %v, want 0", got)
	}
	if got := anomalyRate(baseline, []float64{19}); got != 0 {
		t.Fatalf("at-band-edge (dist 9) rate = %v, want 0 (strict >)", got)
	}
	if got := anomalyRate(baseline, []float64{21}); got != 1 {
		t.Fatalf("out-of-band (dist 11) rate = %v, want 1", got)
	}
}

// shiftMagnitude normalizes the mean shift by scale = |baselineMean| + stddev.
// Baseline {7,13}: mean 10, stddev 3, scale 13. A focus mean 6.5 above baseline
// gives rel = 6.5/13 = 0.5 (a `-` in the scale would give 7 → 0.928); a huge
// shift clamps to 1.
func TestShiftMagnitudeScaleAndClamp(t *testing.T) {
	t.Parallel()
	baseline := []float64{7, 13}
	if got := shiftMagnitude(baseline, []float64{16.5, 16.5}); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("shiftMagnitude = %v, want 0.5", got)
	}
	if got := shiftMagnitude(baseline, []float64{1000, 1000}); got != 1 {
		t.Fatalf("large shift = %v, want clamped to 1", got)
	}
}

// clamp01 floors at 0 and caps at 1. The 0.5 pass-through kills the negation of
// both bounds.
func TestClamp01Bounds(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want float64 }{
		{-0.5, 0}, {0, 0}, {0.5, 0.5}, {1, 1}, {1.5, 1},
	}
	for _, c := range cases {
		if got := clamp01(c.in); got != c.want {
			t.Fatalf("clamp01(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// orDurationDefault substitutes the default for a non-positive value, pinning the
// `<= 0` boundary and negation.
func TestOrDurationDefault(t *testing.T) {
	t.Parallel()
	def := 15 * time.Second
	if got := orDurationDefault(0, def); got != def {
		t.Fatalf("orDurationDefault(0) = %v, want %v", got, def)
	}
	if got := orDurationDefault(-time.Second, def); got != def {
		t.Fatalf("orDurationDefault(-1s) = %v, want %v", got, def)
	}
	if got := orDurationDefault(5*time.Second, def); got != 5*time.Second {
		t.Fatalf("orDurationDefault(5s) = %v, want 5s", got)
	}
}

// normalizeRequest fills TopN with the package default when it is non-positive.
func TestNormalizeRequestDefaultsTopN(t *testing.T) {
	t.Parallel()
	got, err := normalizeRequest(Request{FocusStart: focusStart(), FocusEnd: focusEnd()})
	if err != nil {
		t.Fatalf("normalizeRequest: %v", err)
	}
	if got.TopN != defaultTopN {
		t.Fatalf("normalized TopN = %d, want %d", got.TopN, defaultTopN)
	}
}
