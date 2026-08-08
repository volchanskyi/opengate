package api

import (
	"math"
	"sort"
	"time"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// metricGrid is the time axis of one range response, derived from the request
// alone: exactly span/step instants, every one a whole multiple of the step
// apart. Deriving it from the request rather than from the timestamps the store
// happened to return is what makes the window selector mean something — a 7 d
// request over a device with twenty minutes of data renders seven days with a
// hole, not two points indistinguishable from a 1 h request.
//
// Both edges sit on the step lattice because that is where VictoriaMetrics
// evaluates the query: past a few dozen points it rounds an unaligned start
// down to a whole multiple of the step for its rollup cache, so a grid that
// disagreed by one bucket would shift every value by one bucket. Issuing the
// read at the grid's own edges makes that rounding a no-op and the two agree by
// construction.
type metricGrid struct {
	ts   []int64
	step int64
}

// buildMetricGrid lays out the axis for one (from, to, step) request. The end
// instant is exclusive, so the count is exactly span/step; a window shorter than
// one step still gets a single bucket rather than an empty axis.
func buildMetricGrid(from, to time.Time, step time.Duration) metricGrid {
	stepSecs := int64(step.Seconds())
	if stepSecs < 1 {
		stepSecs = 1
	}
	buckets := int64(to.Sub(from).Seconds()) / stepSecs
	if buckets < 1 {
		buckets = 1
	}

	// Floor the start onto the step lattice, the same rounding VictoriaMetrics
	// applies to a range query's start.
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

// queryStart and queryEnd are the instants the range read is issued at — the
// grid's own first and last bucket, so the answer can only land on the axis the
// response declares.
func (g metricGrid) queryStart() time.Time { return time.Unix(g.ts[0], 0).UTC() }
func (g metricGrid) queryEnd() time.Time   { return time.Unix(g.ts[len(g.ts)-1], 0).UTC() }

// slot locates a timestamp's bucket. A timestamp off the lattice or outside the
// window has no bucket: the caller accounts for it rather than nudging it into
// a neighbour, which would misreport when the value was measured.
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

// offGridPoints accounts for samples that fell outside the request-derived
// grid, keeping the first one so the log can name it. The read path is issued
// at the grid's own instants, so a non-zero count is a defect — and a defect
// that is counted and logged is one that can be found, unlike a value silently
// discarded.
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

// assembleMetricRange projects the avg/min/max series onto the request-derived
// grid and emits one MetricSeries per numeric dimension. Buckets a series lacks
// stay null, which a charting engine renders as a gap. It returns what did not
// fit the grid alongside the response, so the caller can report it.
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

// attachBand fills a series' avg_of_60s band from the per-dim min/max results,
// but only when both are present so a chart never draws a half band. The band
// shares the avg line's grid — a fill on a different axis would not belong to
// the line it wraps.
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

// alignValues projects one series' values onto the grid; absent buckets stay nil
// (JSON null) and off-grid points are recorded rather than dropped. A value the
// wire cannot carry (NaN, ±Inf) leaves its bucket a gap, which is honest — it is
// an unmeasurable reading, not a misplaced one.
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

// chooseStep picks the smallest whole-second bucket (never below the 60 s
// vitals cadence) that keeps the point count within maxPoints for the window.
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
		return true // default: avg_of_60s band
	}
	return *b == GetDeviceMetricsParamsBandAvgOf60s
}
