package vmramseries

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seriesRAMPoint is one load point: the active series held and the resident memory used.
type seriesRAMPoint struct {
	series int
	rss    float64
}

// ramFit is the least-squares line through the load points: marginal bytes per series, fixed
// baseline bytes, and R2.
type ramFit struct {
	bytesPerSeries float64
	baselineBytes  float64
	r2             float64
}

var (
	// errTooFewPoints refuses a fit that a single reading could satisfy.
	errTooFewPoints = errors.New("vmramseries: a fit needs at least two load points")
	// errFlatLoad refuses load points that all sit at one series count, which carry no slope.
	errFlatLoad = errors.New("vmramseries: load points do not vary in series count")
)

// fitSeriesRAM returns the least-squares line of resident memory against active series.
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
	// Equal readings leave nothing to explain, and the line passes through them: a perfect fit.
	r2 := 1.0
	if total > 0 {
		r2 = 1 - residual/total
	}

	return ramFit{bytesPerSeries: slope, baselineBytes: intercept, r2: r2}, nil
}

// projectRAMBytes is the memory the fit predicts for a series count: baseline plus marginal cost.
func (f ramFit) projectRAMBytes(series int) float64 {
	return f.baselineBytes + f.bytesPerSeries*float64(series)
}

// marginalRAMBytes is the fit's per-series cost scaled to a series count, baseline excluded.
func (f ramFit) marginalRAMBytes(series int) float64 {
	return f.bytesPerSeries * float64(series)
}

// naiveBytesPerSeries is one RSS reading divided by its series count, baseline included.
func naiveBytesPerSeries(p seriesRAMPoint) float64 {
	if p.series == 0 {
		return 0
	}
	return p.rss / float64(p.series)
}

// projectDiskBytes projects on-disk cost with one sample per series per cadence tick over the
// retention window.
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

func TestFleetDiskFitsTheBudget(t *testing.T) {
	projected := projectDiskBytes(referenceBytesPerSample, seriesBudget, retentionWindow, vitalsCadence)
	require.LessOrEqual(t, projected, float64(diskBudgetBytes),
		"projected 30 d disk (%.2f GB) must fit the Q4 budget (%.2f GB)",
		projected/gigabyte, float64(diskBudgetBytes)/gigabyte)
}

func TestFleetSeriesMatchTheBudget(t *testing.T) {
	require.Equal(t, 24, seriesPerDevice(), "the vitals a Linux device contributes")
	require.Equal(t, seriesBudget, fleetAgents*seriesPerDevice(),
		"the active-series budget is the per-device cap times the fleet, not an independent number")
}

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
