package api

import (
	"math"
	"sort"
	"time"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// metricGrid is the time axis of a range response, derived from the request alone.
// Its edges sit on the step lattice because VictoriaMetrics rounds an unaligned start down to it.
type metricGrid struct {
	ts   []int64
	step int64
}

// buildMetricGrid lays out span/step buckets with an exclusive end; a shorter window gets one.
func buildMetricGrid(from, to time.Time, step time.Duration) metricGrid {
	stepSecs := int64(step.Seconds())
	if stepSecs < 1 {
		stepSecs = 1
	}
	buckets := int64(to.Sub(from).Seconds()) / stepSecs
	if buckets < 1 {
		buckets = 1
	}

	// The start floors onto the step lattice, as VictoriaMetrics rounds a range query's start.
	rem := from.Unix() % stepSecs
	if rem < 0 {
		rem += stepSecs
	}
	first := from.Unix() - rem

	ts := make([]int64, buckets)
	for i := range ts {
		ts[i] = first + int64(i)*stepSecs
	}
	return metricGrid{ts: ts, step: stepSecs}
}

// queryStart and queryEnd are the grid's first and last bucket, where the range read is issued.
func (g metricGrid) queryStart() time.Time { return time.Unix(g.ts[0], 0).UTC() }
func (g metricGrid) queryEnd() time.Time   { return time.Unix(g.ts[len(g.ts)-1], 0).UTC() }

// slot locates a timestamp's bucket; a timestamp off the lattice or outside the window has none.
func (g metricGrid) slot(ts int64) (int, bool) {
	offset := ts - g.ts[0]
	if offset < 0 || offset%g.step != 0 {
		return 0, false
	}
	i := offset / g.step
	if i >= int64(len(g.ts)) {
		return 0, false
	}
	return int(i), true
}

// offGridPoints counts samples outside the grid and keeps the first one for the log.
type offGridPoints struct {
	count int
	dim   string
	ts    int64
}

func (o *offGridPoints) record(dim string, ts int64) {
	if o.count == 0 {
		o.dim, o.ts = dim, ts
	}
	o.count++
}

// assembleMetricRange projects the avg/min/max series onto the grid; missing buckets stay null.
func assembleMetricRange(avg, mins, maxs []telemetry.RangeSeries, want map[string]bool, wantBand bool, grid metricGrid) (MetricRangeResponse, offGridPoints) {
	minByDim := indexByDim(mins)
	maxByDim := indexByDim(maxs)
	var off offGridPoints

	series := make([]MetricSeries, 0, len(avg))
	for _, a := range avg {
		dim := a.Labels[metricDimLabel]
		if dim == "" || (want != nil && !want[dim]) {
			continue
		}
		ms := MetricSeries{
			Name:         dim,
			Avg:          alignValues(a, dim, grid, &off),
			MinMaxSource: MetricSeriesMinMaxSourceNone,
		}
		if wantBand {
			attachBand(&ms, dim, grid, &off, minByDim, maxByDim)
		}
		series = append(series, ms)
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Name < series[j].Name })

	return MetricRangeResponse{
		T:           grid.ts,
		Series:      series,
		Downsampled: grid.step > minRangeStepSecs,
		BucketS:     int(grid.step),
	}, off
}

// attachBand fills the avg_of_60s band only when both min and max are present.
func attachBand(ms *MetricSeries, dim string, grid metricGrid, off *offGridPoints, minByDim, maxByDim map[string]telemetry.RangeSeries) {
	mn, okMin := minByDim[dim]
	mx, okMax := maxByDim[dim]
	if !okMin || !okMax {
		return
	}
	minVals := alignValues(mn, dim, grid, off)
	maxVals := alignValues(mx, dim, grid, off)
	ms.Min = &minVals
	ms.Max = &maxVals
	ms.MinMaxSource = MetricSeriesMinMaxSourceAvgOf60s
}

// alignValues projects one series onto the grid; absent buckets stay nil and off-grid points are
// recorded. A NaN or ±Inf value leaves its bucket a gap.
func alignValues(s telemetry.RangeSeries, dim string, g metricGrid, off *offGridPoints) []*float64 {
	out := make([]*float64, len(g.ts))
	for i := 0; i < len(s.Timestamps) && i < len(s.Values); i++ {
		pos, ok := g.slot(s.Timestamps[i])
		if !ok {
			off.record(dim, s.Timestamps[i])
			continue
		}
		v := s.Values[i]
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out[pos] = &v
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

// chooseStep picks the smallest whole-second bucket, at least 60 s, that fits maxPoints.
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
		return true
	}
	return *b == GetDeviceMetricsParamsBandAvgOf60s
}
