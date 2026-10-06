package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// Metric names and tuning for the device range endpoint. The dim label carries the numeric
// dimension name.
const (
	metricAvgName         = "opengate_edge_metric_avg"
	metricNodeAnomalyRate = telemetry.MetricNodeAnomalyRate
	metricDimLabel        = "dim"
	metricDeviceIDLabel   = "device_id"
	minRangeStepSecs      = 60   // central vitals cadence — never bucket finer than this
	defaultMaxPoints      = 1000 // chart pixel width order of magnitude
	minMaxPointsBound     = 10
	maxMaxPointsBound     = 2000
	// anomalyBadgeLookback is the window of last_over_time for the badge's anomaly rate; the
	// summary is low-rate, so an instant query at now can miss a recent sample.
	anomalyBadgeLookback = 10 * time.Minute
)

// enrichAnomalyRates fills each device's AnomalyRate from one tenant-scoped instant query.
// It is best-effort: with telemetry disabled, no tenant or a failed query the field stays unset.
func (s *Server) enrichAnomalyRates(ctx context.Context, devices []Device) {
	if s.telemetryReader == nil || len(devices) == 0 {
		return
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return
	}
	vals, err := s.telemetryReader.QueryInstantLookback(ctx, tenant.TenantID, metricNodeAnomalyRate, nil, time.Now(), anomalyBadgeLookback)
	if err != nil {
		s.logger.WarnContext(ctx, "anomaly-rate badge query failed", "error", err)
		return
	}
	byDevice := make(map[string]float64, len(vals))
	for _, v := range vals {
		if id := v.Labels[metricDeviceIDLabel]; id != "" {
			byDevice[id] = v.Value
		}
	}
	for i := range devices {
		if rate, found := byDevice[devices[i].Id.String()]; found {
			r := float32(rate)
			devices[i].AnomalyRate = &r
		}
	}
}

// GetDeviceMetrics implements StrictServerInterface, returning column-oriented downsampled
// telemetry read tenant-scoped, with a bucket width that keeps the points within max_points.
func (s *Server) GetDeviceMetrics(ctx context.Context, request GetDeviceMetricsRequestObject) (GetDeviceMetricsResponseObject, error) {
	if s.telemetryReader == nil {
		return GetDeviceMetrics503JSONResponse{Error: "telemetry not available"}, nil
	}

	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceMetrics404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return GetDeviceMetrics403JSONResponse{Error: msgForbidden}, nil
	}

	from := request.Params.From
	to := request.Params.To
	if !to.After(from) {
		return GetDeviceMetrics400JSONResponse{Error: "to must be after from"}, nil
	}

	maxPoints := clampMaxPoints(request.Params.MaxPoints)
	step := chooseStep(from, to, maxPoints)
	wantBand := bandFromParam(request.Params.Band)

	resp, err := s.buildMetricRange(ctx, tenant.TenantID, request.Id, metricRangeQuery{
		from: from, to: to, step: step, dims: request.Params.Dims, wantBand: wantBand,
	})
	if err != nil {
		return GetDeviceMetrics503JSONResponse{Error: "telemetry query failed"}, nil
	}
	return GetDeviceMetrics200JSONResponse(resp), nil
}

type metricRangeQuery struct {
	from, to time.Time
	step     time.Duration
	dims     *[]string
	wantBand bool
}

// buildMetricRange fetches the avg line and optional avg_of_60s band per dimension and projects
// each series onto one grid, which also bounds the range reads and is the response's time axis.
func (s *Server) buildMetricRange(ctx context.Context, tenantID, deviceID uuid.UUID, q metricRangeQuery) (MetricRangeResponse, error) {
	grid := buildMetricGrid(q.from, q.to, q.step)
	matchers := map[string]string{"device_id": deviceID.String()}
	read := func(agg telemetry.RangeAgg) ([]telemetry.RangeSeries, error) {
		return s.telemetryReader.QueryRange(ctx, tenantID, telemetry.RangeQuery{
			Metric: metricAvgName, Matchers: matchers, Agg: agg,
			Start: grid.queryStart(), End: grid.queryEnd(), Step: q.step,
		})
	}

	avg, err := read(telemetry.RangeAvg)
	if err != nil {
		return MetricRangeResponse{}, err
	}

	var mins, maxs []telemetry.RangeSeries
	if q.wantBand {
		if mins, err = read(telemetry.RangeMin); err != nil {
			return MetricRangeResponse{}, err
		}
		if maxs, err = read(telemetry.RangeMax); err != nil {
			return MetricRangeResponse{}, err
		}
	}

	resp, off := assembleMetricRange(avg, mins, maxs, dimFilter(q.dims), q.wantBand, grid)
	s.reportGridMisalignment(ctx, deviceID, grid, off)
	return resp, nil
}

// reportGridMisalignment counts and logs samples that fall outside the grid of their own
// query, since placing them would misreport when they were measured.
func (s *Server) reportGridMisalignment(ctx context.Context, deviceID uuid.UUID, grid metricGrid, off offGridPoints) {
	if off.count == 0 {
		return
	}
	if s.metrics != nil {
		s.metrics.ObserveMetricsGridMisalignment(off.count)
	}
	s.logger.WarnContext(ctx, "telemetry samples fell outside the request grid",
		"device_id", deviceID,
		"points", off.count,
		"dim", off.dim,
		"first_off_grid_ts", off.ts,
		"grid_start", grid.ts[0],
		"grid_buckets", len(grid.ts),
		"step_s", grid.step)
}
