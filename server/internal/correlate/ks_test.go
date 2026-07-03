package correlate

import (
	"math"
	"testing"
)

func TestKSStatistic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		a, b    []float64
		wantMin float64 // D must be >= this
		wantMax float64 // D must be <= this
	}{
		{
			name:    "identical distributions give near-zero D",
			a:       []float64{1, 2, 3, 4, 5},
			b:       []float64{1, 2, 3, 4, 5},
			wantMin: 0,
			wantMax: 0.001,
		},
		{
			name:    "fully separated distributions give D=1",
			a:       []float64{1, 1, 1, 1},
			b:       []float64{9, 9, 9, 9},
			wantMin: 0.999,
			wantMax: 1.0,
		},
		{
			name:    "partial shift gives intermediate D",
			a:       []float64{1, 2, 3, 4},
			b:       []float64{3, 4, 5, 6},
			wantMin: 0.4,
			wantMax: 0.6,
		},
		{
			name:    "empty sample yields zero",
			a:       nil,
			b:       []float64{1, 2, 3},
			wantMin: 0,
			wantMax: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ksStatistic(tc.a, tc.b)
			if got < tc.wantMin || got > tc.wantMax {
				t.Fatalf("ksStatistic = %v, want in [%v, %v]", got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestKSStatisticIsSymmetric(t *testing.T) {
	t.Parallel()
	a := []float64{1, 3, 3, 7, 9, 10}
	b := []float64{2, 2, 4, 8, 8}
	if d1, d2 := ksStatistic(a, b), ksStatistic(b, a); math.Abs(d1-d2) > 1e-9 {
		t.Fatalf("KS statistic not symmetric: %v vs %v", d1, d2)
	}
}

func TestAnomalyRate(t *testing.T) {
	t.Parallel()
	baseline := []float64{10, 10, 10, 10, 10, 10, 10, 10}

	t.Run("flat focus matching baseline is not anomalous", func(t *testing.T) {
		t.Parallel()
		if got := anomalyRate(baseline, []float64{10, 10, 10, 10}); got != 0 {
			t.Fatalf("anomalyRate = %v, want 0", got)
		}
	})

	t.Run("all-shifted focus is fully anomalous under zero-variance baseline", func(t *testing.T) {
		t.Parallel()
		if got := anomalyRate(baseline, []float64{99, 99, 99, 99}); got != 1 {
			t.Fatalf("anomalyRate = %v, want 1", got)
		}
	})

	t.Run("wide-band baseline tolerates small excursions", func(t *testing.T) {
		t.Parallel()
		noisy := []float64{8, 12, 9, 11, 10, 10, 8, 12, 9, 11}
		// Focus values within the baseline mean±3σ band should not count.
		if got := anomalyRate(noisy, []float64{10, 11, 9, 10}); got != 0 {
			t.Fatalf("anomalyRate = %v, want 0 for in-band focus", got)
		}
		// A large excursion beyond the band counts.
		if got := anomalyRate(noisy, []float64{10, 10, 10, 500}); got != 0.25 {
			t.Fatalf("anomalyRate = %v, want 0.25 for one-of-four out-of-band", got)
		}
	})

	t.Run("empty focus yields zero", func(t *testing.T) {
		t.Parallel()
		if got := anomalyRate(baseline, nil); got != 0 {
			t.Fatalf("anomalyRate = %v, want 0", got)
		}
	})
}
