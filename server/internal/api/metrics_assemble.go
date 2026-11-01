package api

import (
	"math"
	"sort"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

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

func bandFromParam(b *GetDeviceMetricsParamsBand) bool {
	if b == nil {
		return true
	}
	return *b == GetDeviceMetricsParamsBandAvgOf60s
}
