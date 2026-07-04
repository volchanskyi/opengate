package api

import (
	"math"
	"sort"
	"time"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// assembleMetricRange aligns avg/min/max series onto the union timestamp grid and
// emits one MetricSeries per numeric dimension. Buckets a series lacks become
// null, which a charting engine renders as a gap.
func assembleMetricRange(avg, mins, maxs []telemetry.RangeSeries, want map[string]bool, wantBand bool, stepSecs int) MetricRangeResponse {
	grid, index := unionGrid(avg)
	minByDim := indexByDim(mins)
	maxByDim := indexByDim(maxs)

	series := make([]MetricSeries, 0, len(avg))
	for _, a := range avg {
		dim := a.Labels[metricDimLabel]
		if dim == "" || (want != nil && !want[dim]) {
			continue
		}
		ms := MetricSeries{
			Name:         dim,
			Avg:          alignValues(a, grid, index),
			MinMaxSource: MetricSeriesMinMaxSourceNone,
		}
		if wantBand {
			attachBand(&ms, dim, grid, index, minByDim, maxByDim)
		}
		series = append(series, ms)
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Name < series[j].Name })

	return MetricRangeResponse{
		T:           grid,
		Series:      series,
		Downsampled: stepSecs > minRangeStepSecs,
		BucketS:     stepSecs,
	}
}

// attachBand fills a series' avg_of_10s band from the per-dim min/max results,
// but only when both are present so a chart never draws a half band.
func attachBand(ms *MetricSeries, dim string, grid []int64, index map[int64]int, minByDim, maxByDim map[string]telemetry.RangeSeries) {
	mn, okMin := minByDim[dim]
	mx, okMax := maxByDim[dim]
	if !okMin || !okMax {
		return
	}
	minVals := alignValues(mn, grid, index)
	maxVals := alignValues(mx, grid, index)
	ms.Min = &minVals
	ms.Max = &maxVals
	ms.MinMaxSource = MetricSeriesMinMaxSourceAvgOf10s
}

// unionGrid returns the sorted unique timestamps across all series and a lookup
// from timestamp to its index in that grid.
func unionGrid(series []telemetry.RangeSeries) ([]int64, map[int64]int) {
	seen := map[int64]struct{}{}
	for _, s := range series {
		for _, ts := range s.Timestamps {
			seen[ts] = struct{}{}
		}
	}
	grid := make([]int64, 0, len(seen))
	for ts := range seen {
		grid = append(grid, ts)
	}
	sort.Slice(grid, func(i, j int) bool { return grid[i] < grid[j] })
	index := make(map[int64]int, len(grid))
	for i, ts := range grid {
		index[ts] = i
	}
	return grid, index
}

// alignValues projects one series' values onto the shared grid; absent buckets
// stay nil (JSON null).
func alignValues(s telemetry.RangeSeries, grid []int64, index map[int64]int) []*float64 {
	out := make([]*float64, len(grid))
	for i, ts := range s.Timestamps {
		if pos, ok := index[ts]; ok {
			v := s.Values[i]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			out[pos] = &v
		}
	}
	return out
}

func indexByDim(series []telemetry.RangeSeries) map[string]telemetry.RangeSeries {
	out := make(map[string]telemetry.RangeSeries, len(series))
	for _, s := range series {
		if dim := s.Labels[metricDimLabel]; dim != "" {
			out[dim] = s
		}
	}
	return out
}

func dimFilter(dims *[]string) map[string]bool {
	if dims == nil || len(*dims) == 0 {
		return nil
	}
	want := make(map[string]bool, len(*dims))
	for _, d := range *dims {
		if d != "" {
			want[d] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	return want
}

func clampMaxPoints(mp *int) int {
	v := defaultMaxPoints
	if mp != nil {
		v = *mp
	}
	if v < minMaxPointsBound {
		return minMaxPointsBound
	}
	if v > maxMaxPointsBound {
		return maxMaxPointsBound
	}
	return v
}

// chooseStep picks the smallest whole-second bucket (never below the 10 s raw
// cadence) that keeps the point count within maxPoints for the window.
func chooseStep(from, to time.Time, maxPoints int) time.Duration {
	windowSecs := int64(to.Sub(from).Seconds())
	if windowSecs <= 0 {
		return minRangeStepSecs * time.Second
	}
	step := (windowSecs + int64(maxPoints) - 1) / int64(maxPoints)
	if step < minRangeStepSecs {
		step = minRangeStepSecs
	}
	return time.Duration(step) * time.Second
}

func bandFromParam(b *GetDeviceMetricsParamsBand) bool {
	if b == nil {
		return true // default: avg_of_10s band
	}
	return *b == GetDeviceMetricsParamsBandAvgOf10s
}
