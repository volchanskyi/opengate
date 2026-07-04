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

// Metric names and tuning for the device range endpoint. The dim label carries
// the numeric dimension name; process basenames live only in the RLS table and
// are never charted.
const (
	metricAvgName         = "opengate_edge_metric_avg"
	metricNodeAnomalyRate = "opengate_edge_node_anomaly_rate"
	metricDimLabel        = "dim"
	metricDeviceIDLabel   = "device_id"
	minRangeStepSecs      = 10   // raw 10 s sample cadence — never bucket finer than this
	defaultMaxPoints      = 1000 // chart pixel width order of magnitude
	minMaxPointsBound     = 10
	maxMaxPointsBound     = 2000
)

// enrichAnomalyRates fills each device's AnomalyRate from the latest node
// anomaly-rate sample in VictoriaMetrics via a single tenant-scoped instant
// query. It is best-effort: when telemetry is disabled, the tenant is unknown,
// or the query fails, the field is simply left unset and the list still returns.
func (s *Server) enrichAnomalyRates(ctx context.Context, devices []Device) {
	if s.telemetryReader == nil || len(devices) == 0 {
		return
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return
	}
	vals, err := s.telemetryReader.QueryInstant(ctx, tenant.OrgID, metricNodeAnomalyRate, nil, time.Now())
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

// GetDeviceMetrics implements StrictServerInterface. It returns column-oriented
// downsampled numeric telemetry for a device window, read tenant-scoped from
// VictoriaMetrics with a bucket width chosen so the point count stays within
// max_points regardless of window span.
func (s *Server) GetDeviceMetrics(ctx context.Context, request GetDeviceMetricsRequestObject) (GetDeviceMetricsResponseObject, error) {
	if s.telemetryReader == nil {
		return GetDeviceMetrics503JSONResponse{Error: "telemetry not available"}, nil
	}

	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceMetrics404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}
	if !s.isGroupOwner(ctx, d.GroupID) {
		return GetDeviceMetrics403JSONResponse{Error: msgForbidden}, nil
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

	resp, err := s.buildMetricRange(ctx, tenant.OrgID, request.Id, metricRangeQuery{
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

// buildMetricRange fetches the avg line (and optional avg_of_10s band) for the
// device's numeric dimensions and aligns every series onto one timestamp grid so
// the payload maps 1:1 to a client charting engine's aligned data.
func (s *Server) buildMetricRange(ctx context.Context, orgID, deviceID uuid.UUID, q metricRangeQuery) (MetricRangeResponse, error) {
	matchers := map[string]string{"device_id": deviceID.String()}
	avg, err := s.telemetryReader.QueryRange(ctx, orgID, telemetry.RangeQuery{
		Metric: metricAvgName, Matchers: matchers, Agg: telemetry.RangeAvg,
		Start: q.from, End: q.to, Step: q.step,
	})
	if err != nil {
		return MetricRangeResponse{}, err
	}

	var mins, maxs []telemetry.RangeSeries
	if q.wantBand {
		if mins, err = s.telemetryReader.QueryRange(ctx, orgID, telemetry.RangeQuery{
			Metric: metricAvgName, Matchers: matchers, Agg: telemetry.RangeMin,
			Start: q.from, End: q.to, Step: q.step,
		}); err != nil {
			return MetricRangeResponse{}, err
		}
		if maxs, err = s.telemetryReader.QueryRange(ctx, orgID, telemetry.RangeQuery{
			Metric: metricAvgName, Matchers: matchers, Agg: telemetry.RangeMax,
			Start: q.from, End: q.to, Step: q.step,
		}); err != nil {
			return MetricRangeResponse{}, err
		}
	}

	stepSecs := int(q.step.Seconds())
	return assembleMetricRange(avg, mins, maxs, dimFilter(q.dims), q.wantBand, stepSecs), nil
}
