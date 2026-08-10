package vmramseries

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The arithmetic behind the measurement, kept separate from the VictoriaMetrics
// half so the method itself is provable without a container: a line through the
// load points, the two projections taken off it, and the disk projection.

// seriesRAMPoint is one load point of the experiment — the active series
// VictoriaMetrics held, and the resident memory its process used while holding
// them.
type seriesRAMPoint struct {
	series int
	rss    float64
}

// ramFit is the least-squares line through the load points. bytesPerSeries is
// the marginal cost of one more active series and is the number that scales;
// baselineBytes is VictoriaMetrics' fixed cost, which exists whether the store
// holds one series or a hundred thousand.
type ramFit struct {
	bytesPerSeries float64
	baselineBytes  float64
	r2             float64
}

var (
	// errTooFewPoints refuses a fit that a single reading could satisfy.
	errTooFewPoints = errors.New("vmramseries: a fit needs at least two load points")
	// errFlatLoad refuses load points that all sit at the same series count:
	// there is no slope to recover from readings taken at one x.
	errFlatLoad = errors.New("vmramseries: load points do not vary in series count")
)

// fitSeriesRAM returns the least-squares line of resident memory against active
// series.
//
// The slope is the deliverable. Dividing a single RSS reading by the series held
// at that moment answers a different question — it charges VictoriaMetrics'
// whole baseline to the series that happen to be present, which overstates the
// per-series cost by the baseline divided by N and so lands furthest from the
// truth exactly where the store is smallest.
func fitSeriesRAM(points []seriesRAMPoint) (ramFit, error) {
	if len(points) < 2 {
		return ramFit{}, errTooFewPoints
	}

	n := float64(len(points))
	var sumX, sumY float64
	for _, p := range points {
		sumX += float64(p.series)
		sumY += p.rss
	}
	meanX, meanY := sumX/n, sumY/n

	var varX, covXY float64
	for _, p := range points {
		dx := float64(p.series) - meanX
		varX += dx * dx
		covXY += dx * (p.rss - meanY)
	}
	if varX == 0 {
		return ramFit{}, errFlatLoad
	}

	slope := covXY / varX
	intercept := meanY - slope*meanX

	var residual, total float64
	for _, p := range points {
		d := p.rss - (intercept + slope*float64(p.series))
		residual += d * d
		dy := p.rss - meanY
		total += dy * dy
	}
	// Readings that are all equal leave nothing for the line to explain, and the
	// line passes through every one of them exactly — a perfect fit, not an
	// undefined one.
	r2 := 1.0
	if total > 0 {
		r2 = 1 - residual/total
	}

	return ramFit{bytesPerSeries: slope, baselineBytes: intercept, r2: r2}, nil
}

// projectRAMBytes is the memory the fit predicts VictoriaMetrics needs to hold a
// series count: baseline plus marginal cost. This is the figure the Q3 budget
// bounds, because a pod pays for the baseline too.
func (f ramFit) projectRAMBytes(series int) float64 {
	return f.baselineBytes + f.bytesPerSeries*float64(series)
}

// marginalRAMBytes is the fit's per-series cost scaled to a series count, with
// the baseline left out — the form the sizing expectation table is stated in, so
// the measurement can be read straight against it.
func (f ramFit) marginalRAMBytes(series int) float64 {
	return f.bytesPerSeries * float64(series)
}

// naiveBytesPerSeries is what a single RSS reading divided by its series count
// claims the per-series cost is. It exists to be measured against the fit: the
// gap between the two is the baseline this experiment was written to separate
// out.
func naiveBytesPerSeries(p seriesRAMPoint) float64 {
	if p.series == 0 {
		return 0
	}
	return p.rss / float64(p.series)
}

// projectDiskBytes projects on-disk cost from a measured bytes-per-sample
// figure: every active series contributes one sample per cadence tick for the
// whole retention window.
func projectDiskBytes(bytesPerSample float64, activeSeries int, retention, cadence time.Duration) float64 {
	samplesPerSeries := float64(retention) / float64(cadence)
	return bytesPerSample * samplesPerSeries * float64(activeSeries)
}

func TestFitSeriesRAM(t *testing.T) {
	tests := []struct {
		name          string
		points        []seriesRAMPoint
		wantErr       error
		wantSlope     float64
		wantBaseline  float64
		wantR2        float64
		slopeTol      float64
		baselineTol   float64
		r2Tol         float64
		wantR2AtLeast float64
	}{
		{
			// 80 MB of baseline plus 2 KB per series, read without noise.
			name: "recovers slope and baseline from an exact line",
			points: []seriesRAMPoint{
				{series: 10_000, rss: 80*megabyte + 10_000*2000},
				{series: 20_000, rss: 80*megabyte + 20_000*2000},
				{series: 30_000, rss: 80*megabyte + 30_000*2000},
				{series: 40_000, rss: 80*megabyte + 40_000*2000},
			},
			wantSlope:    2000,
			wantBaseline: 80 * megabyte,
			wantR2:       1,
		},
		{
			name: "tolerates reading noise and reports it in R2",
			points: []seriesRAMPoint{
				{series: 10_000, rss: 80*megabyte + 10_000*2000 + 3*megabyte},
				{series: 20_000, rss: 80*megabyte + 20_000*2000 - 2*megabyte},
				{series: 30_000, rss: 80*megabyte + 30_000*2000 + 1*megabyte},
				{series: 40_000, rss: 80*megabyte + 40_000*2000 - 1*megabyte},
			},
			wantSlope:     2000,
			slopeTol:      120,
			wantBaseline:  80 * megabyte,
			baselineTol:   4 * megabyte,
			wantR2AtLeast: 0.99,
		},
		{
			// A store whose memory does not move with series count has a real
			// answer — zero marginal cost — and the line explains it exactly.
			name: "reports a flat store as zero marginal cost",
			points: []seriesRAMPoint{
				{series: 10_000, rss: 80 * megabyte},
				{series: 20_000, rss: 80 * megabyte},
				{series: 30_000, rss: 80 * megabyte},
			},
			wantSlope:    0,
			wantBaseline: 80 * megabyte,
			wantR2:       1,
		},
		{
			name:    "refuses a single load point",
			points:  []seriesRAMPoint{{series: 10_000, rss: 100 * megabyte}},
			wantErr: errTooFewPoints,
		},
		{
			name:    "refuses no load points at all",
			points:  nil,
			wantErr: errTooFewPoints,
		},
		{
			name: "refuses readings taken at one series count",
			points: []seriesRAMPoint{
				{series: 10_000, rss: 100 * megabyte},
				{series: 10_000, rss: 140 * megabyte},
				{series: 10_000, rss: 120 * megabyte},
			},
			wantErr: errFlatLoad,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fitSeriesRAM(tt.points)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tt.wantSlope, got.bytesPerSeries, tt.slopeTol)
			require.InDelta(t, tt.wantBaseline, got.baselineBytes, tt.baselineTol)
			if tt.wantR2AtLeast > 0 {
				require.GreaterOrEqual(t, got.r2, tt.wantR2AtLeast)
				require.LessOrEqual(t, got.r2, 1.0)
				return
			}
			require.InDelta(t, tt.wantR2, got.r2, tt.r2Tol)
		})
	}
}

// TestFitBeatsSinglePointDivision is the reason this package fits a line instead
// of dividing. Both methods see the same store — 80 MB of baseline and 2 KB per
// series — and the division answers 10 KB at the smallest load point, five times
// the truth, converging only as the baseline is diluted by series that a test
// harness cannot afford to write.
func TestFitBeatsSinglePointDivision(t *testing.T) {
	const (
		baseline  = 80 * megabyte
		perSeries = 2000.0
	)
	points := []seriesRAMPoint{
		{series: 10_000, rss: baseline + 10_000*perSeries},
		{series: 20_000, rss: baseline + 20_000*perSeries},
		{series: 30_000, rss: baseline + 30_000*perSeries},
		{series: 40_000, rss: baseline + 40_000*perSeries},
	}

	got, err := fitSeriesRAM(points)
	require.NoError(t, err)
	require.InDelta(t, perSeries, got.bytesPerSeries, 1)

	naive := naiveBytesPerSeries(points[0])
	require.InDelta(t, 10_000, naive, 1, "division charges the whole baseline to 10 000 series")
	require.Greater(t, naive, 4*got.bytesPerSeries,
		"the single-point answer must be visibly wrong, or this test proves nothing")

	// The error shrinks with N, which is why a harness that can only afford a
	// small store has to fit rather than divide.
	require.Less(t, naiveBytesPerSeries(points[3]), naive)
	require.Greater(t, naiveBytesPerSeries(points[3]), got.bytesPerSeries)
}

func TestRAMProjections(t *testing.T) {
	fit := ramFit{bytesPerSeries: 2000, baselineBytes: 80 * megabyte}

	require.InDelta(t, 240*megabyte, fit.marginalRAMBytes(seriesBudget), megabyte,
		"2 KB per series at the Q2 budget is the ~240 MB the sizing table states")
	require.InDelta(t, 320*megabyte, fit.projectRAMBytes(seriesBudget), megabyte,
		"the pod also pays the baseline, which is what the Q3 budget bounds")
	require.Less(t, fit.projectRAMBytes(seriesBudget), float64(ramBudgetBytes),
		"the derived figure fits the Q3 budget — the experiment exists to check whether the real one does")
}

func TestProjectDiskBytes(t *testing.T) {
	tests := []struct {
		name           string
		bytesPerSample float64
		series         int
		want           float64
		tol            float64
	}{
		{
			// The central store's own measured cost per sample, scaled to the
			// fleet: 30 d at 60 s is 43 200 samples on each of 120 000 series.
			name:           "reference fleet at the measured cost per sample",
			bytesPerSample: referenceBytesPerSample,
			series:         seriesBudget,
			want:           1.638e9,
			tol:            5e6,
		},
		{
			name:           "halving the series halves the disk",
			bytesPerSample: referenceBytesPerSample,
			series:         seriesBudget / 2,
			want:           0.819e9,
			tol:            5e6,
		},
		{
			name:           "an empty store costs nothing",
			bytesPerSample: referenceBytesPerSample,
			series:         0,
			want:           0,
			tol:            0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectDiskBytes(tt.bytesPerSample, tt.series, retentionWindow, vitalsCadence)
			require.InDelta(t, tt.want, got, tt.tol)
		})
	}
}

// TestFleetDiskFitsTheBudget states Q4 as arithmetic over the measured cost per
// sample, so the fleet-scale figure the harness produces has a stated
// expectation to be read against rather than being the first number anyone sees.
func TestFleetDiskFitsTheBudget(t *testing.T) {
	projected := projectDiskBytes(referenceBytesPerSample, seriesBudget, retentionWindow, vitalsCadence)
	require.LessOrEqual(t, projected, float64(diskBudgetBytes),
		"projected 30 d disk (%.2f GB) must fit the Q4 budget (%.2f GB)",
		projected/gigabyte, float64(diskBudgetBytes)/gigabyte)
}

// TestFleetSeriesMatchTheBudget pins Q2 as the product it is derived from, so the
// budget and the per-device cap can never drift apart.
func TestFleetSeriesMatchTheBudget(t *testing.T) {
	require.Equal(t, 24, seriesPerDevice(), "the vitals a Linux device contributes")
	require.Equal(t, seriesBudget, fleetAgents*seriesPerDevice(),
		"the active-series budget is the per-device cap times the fleet, not an independent number")
}

// TestVitalsSetIsTheSizingShape pins what the harness writes. Sizing assumes
// every device contributes the full 24 — a capacity plan must not assume the
// cheaper platform mix — so the harness writes all of them, including the eight
// Linux-only vitals the cap reserves room for.
func TestVitalsSetIsTheSizingShape(t *testing.T) {
	require.Len(t, vitalDims, 18)
	require.Len(t, anomalyFamilies, 5)
	require.Equal(t, 24, seriesPerDevice())

	seen := make(map[string]bool, len(vitalDims))
	for _, dim := range vitalDims {
		require.False(t, seen[dim], "duplicate dim %q would write one series, not two", dim)
		seen[dim] = true
	}

	for _, dim := range []string{
		"cpu.total", "cpu.total.max", "disk.used_percent", "disk.mounts_critical",
		"disk.await_ms", "disk.await_ms.max", "disk.queue_depth",
		"stall.cpu.some", "stall.mem.some", "stall.mem.full", "stall.io.some", "stall.io.full",
	} {
		require.True(t, seen[dim], "the sizing shape must include %q", dim)
	}
}

func TestLoadPoints(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []int
		wantErr bool
	}{
		{name: "empty spec falls back to the always-on scale", spec: "", want: defaultLoadPoints},
		{name: "parses the fleet scale", spec: "2000,3000,4000,5000", want: []int{2000, 3000, 4000, 5000}},
		{name: "tolerates spacing", spec: " 1250 , 1500,1750,2000 ", want: []int{1250, 1500, 1750, 2000}},
		{name: "refuses fewer than four points", spec: "1250,1500,1750", wantErr: true},
		{name: "refuses a non-increasing scale", spec: "1250,1500,1500,2000", wantErr: true},
		{name: "refuses a first point inside the warm-up", spec: "500,1500,1750,2000", wantErr: true},
		{name: "refuses a zero point", spec: "0,1500,1750,2000", wantErr: true},
		{name: "refuses a non-numeric point", spec: "1250,fifteen hundred,1750,2000", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLoadPoints(tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name string
		in   []float64
		want float64
	}{
		{name: "odd count takes the middle reading", in: []float64{30, 10, 20}, want: 20},
		{name: "even count averages the middle pair", in: []float64{40, 10, 30, 20}, want: 25},
		{name: "one reading is its own median", in: []float64{7}, want: 7},
		{name: "a garbage-collection spike does not move it", in: []float64{100, 101, 260}, want: 101},
		{name: "no readings", in: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := append([]float64(nil), tt.in...)
			require.InDelta(t, tt.want, median(in), 1e-9)
			require.Equal(t, tt.in, in, "median must not reorder the caller's readings")
			require.False(t, math.IsNaN(median(in)))
		})
	}
}
