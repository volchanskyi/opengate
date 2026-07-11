package api

import (
	"context"
	"errors"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// historyFetchTimeout bounds how long a deep-history pull may block on the agent.
const historyFetchTimeout = 15 * time.Second

const (
	defaultHistoryMaxPoints = 1000
	minHistoryMaxPoints     = 10
	maxHistoryMaxPoints     = 20000
)

// GetDeviceHistory brokers an on-demand, full-resolution local-history pull for a
// single dimension from the connected agent's local store — the deep history that
// central VictoriaMetrics (avg-only) does not keep. It is single-host and
// server-mediated (no browser-to-agent access, no fan-out), bounded by the window
// and the max_points cap. Access is device-scoped: a caller who does not own the
// device's group is denied, so a history pull can never cross tenants.
func (s *Server) GetDeviceHistory(ctx context.Context, request GetDeviceHistoryRequestObject) (GetDeviceHistoryResponseObject, error) {
	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceHistory404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}
	if !s.isGroupOwner(ctx, d.GroupID) {
		return GetDeviceHistory403JSONResponse{Error: msgForbidden}, nil
	}

	dim := request.Params.Dim
	if dim == "" {
		return GetDeviceHistory400JSONResponse{Error: "dim is required"}, nil
	}
	from, to := request.Params.From, request.Params.To
	if !to.After(from) {
		return GetDeviceHistory400JSONResponse{Error: "to must be after from"}, nil
	}
	maxPoints := clampHistoryMaxPoints(request.Params.MaxPoints)

	ac := s.agents.GetAgent(request.Id)
	if ac == nil {
		return GetDeviceHistory404JSONResponse{Error: "history not available — device offline"}, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, historyFetchTimeout)
	defer cancel()
	points, truncated, err := ac.RequestLocalHistorySync(fetchCtx, dim, from.Unix(), to.Unix(), maxPoints)
	if err != nil {
		return historyBrokerErrorResponse(err)
	}

	return GetDeviceHistory200JSONResponse(DeviceHistoryResponse{
		Dim:       dim,
		Points:    historyPointsToAPI(points),
		Truncated: truncated,
	}), nil
}

// clampHistoryMaxPoints applies the endpoint's default and bounds to the caller's
// optional max_points so a pull can never be unbounded.
func clampHistoryMaxPoints(p *int) uint32 {
	v := defaultHistoryMaxPoints
	if p != nil {
		v = *p
	}
	return uint32(min(max(v, minHistoryMaxPoints), maxHistoryMaxPoints))
}

func historyPointsToAPI(points []protocol.HistoryPoint) []HistoryPoint {
	out := make([]HistoryPoint, len(points))
	for i, p := range points {
		out[i] = HistoryPoint{Ts: p.TS, Value: p.Value}
	}
	return out
}

// historyBrokerErrorResponse maps broker failures to bounded HTTP responses:
// unsupported agents and busy/timeout conditions are client-visible, everything
// else is a 500.
func historyBrokerErrorResponse(err error) (GetDeviceHistoryResponseObject, error) {
	switch {
	case agentapi.IsCapabilityError(err):
		return GetDeviceHistory404JSONResponse{Error: "history not available"}, nil
	case errors.Is(err, agentapi.ErrHistoryBusy):
		return GetDeviceHistory409JSONResponse{Error: "a history request is already in progress for this device"}, nil
	case errors.Is(err, context.DeadlineExceeded):
		return GetDeviceHistory504JSONResponse{Error: "device did not return history in time"}, nil
	default:
		return nil, err
	}
}
