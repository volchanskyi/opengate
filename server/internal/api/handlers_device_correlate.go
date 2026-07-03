package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/correlate"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// CorrelateDevice implements StrictServerInterface. It ranks the device's
// anomalous metric dimensions for a focus window versus a baseline, reading
// telemetry only through the tenant-scoped VM client (org derived from the
// authenticated context, never the request body).
func (s *Server) CorrelateDevice(ctx context.Context, request CorrelateDeviceRequestObject) (CorrelateDeviceResponseObject, error) {
	if s.correlate == nil {
		return CorrelateDevice503JSONResponse{Error: "correlation not available"}, nil
	}

	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return CorrelateDevice404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}
	if !s.isGroupOwner(ctx, d.GroupID) {
		return CorrelateDevice403JSONResponse{Error: msgForbidden}, nil
	}

	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return CorrelateDevice403JSONResponse{Error: msgForbidden}, nil
	}

	if request.Body == nil {
		return CorrelateDevice400JSONResponse{Error: "request body required"}, nil
	}
	req := correlate.Request{
		DeviceID:   request.Id,
		FocusStart: request.Body.FocusStart,
		FocusEnd:   request.Body.FocusEnd,
	}
	if request.Body.BaselineStart != nil {
		req.BaselineStart = *request.Body.BaselineStart
	}
	if request.Body.BaselineEnd != nil {
		req.BaselineEnd = *request.Body.BaselineEnd
	}
	if request.Body.TopN != nil {
		req.TopN = *request.Body.TopN
	}

	res, err := s.correlate.Correlate(ctx, tenant.OrgID, req)
	switch {
	case err == nil:
		return CorrelateDevice200JSONResponse(correlationResultToAPI(res)), nil
	case errors.Is(err, correlate.ErrInvalidWindow):
		return CorrelateDevice400JSONResponse{Error: "invalid window"}, nil
	case errors.Is(err, correlate.ErrBusy):
		return CorrelateDevice503JSONResponse{Error: "correlation at capacity, retry shortly"}, nil
	default:
		return nil, err
	}
}

func correlationResultToAPI(res correlate.Result) CorrelateResponse {
	ranked := make([]CorrelatedDimension, 0, len(res.Ranked))
	for _, r := range res.Ranked {
		dim := CorrelatedDimension{
			Metric:          r.Metric,
			Score:           float32(r.Score),
			KsStatistic:     float32(r.KSStatistic),
			AnomalyRate:     float32(r.AnomalyRate),
			ShiftMagnitude:  float32(r.ShiftMagnitude),
			BaselineSamples: r.BaselineSamples,
			FocusSamples:    r.FocusSamples,
		}
		if len(r.Labels) > 0 {
			labels := make(map[string]string, len(r.Labels))
			for k, v := range r.Labels {
				labels[k] = v
			}
			dim.Labels = &labels
		}
		ranked = append(ranked, dim)
	}
	return CorrelateResponse{
		Ranked:           ranked,
		SeriesConsidered: res.SeriesConsidered,
		SeriesTruncated:  res.SeriesTruncated,
	}
}
