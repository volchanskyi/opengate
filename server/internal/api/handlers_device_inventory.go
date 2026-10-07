package api

import (
	"context"
	"errors"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/metrics"
)

// GetDeviceHardware implements StrictServerInterface.
func (s *Server) GetDeviceHardware(ctx context.Context, request GetDeviceHardwareRequestObject) (GetDeviceHardwareResponseObject, error) {
	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceHardware404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	hw, err := s.hardware.Get(ctx, request.Id)
	if err == nil {
		return GetDeviceHardware200JSONResponse(deviceHardwareToAPI(hw)), nil
	}
	if !errors.Is(err, device.ErrHardwareNotFound) {
		return nil, err
	}
	return s.requestHardwareFromAgent(ctx, request.Id)
}

func (s *Server) requestHardwareFromAgent(ctx context.Context, id device.DeviceID) (GetDeviceHardwareResponseObject, error) {
	ac := s.agents.GetAgent(id)
	if ac == nil {
		return GetDeviceHardware404JSONResponse{Error: "hardware info not available"}, nil
	}
	if err := ac.SendRequestHardwareReport(ctx); err != nil {
		if agentapi.IsCapabilityError(err) {
			return GetDeviceHardware404JSONResponse{Error: "hardware info not available"}, nil
		}
		return nil, err
	}
	return GetDeviceHardware202Response{}, nil
}

// GetDeviceInventory implements StrictServerInterface, returning the device's discovered
// footprint from the tenant-scoped inventory store to any tenant member.
func (s *Server) GetDeviceInventory(ctx context.Context, request GetDeviceInventoryRequestObject) (GetDeviceInventoryResponseObject, error) {
	if s.inventory == nil {
		return GetDeviceInventory503JSONResponse{Error: "inventory not available"}, nil
	}

	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceInventory404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	components, err := s.inventory.ListForDevice(ctx, request.Id, 0)
	if err != nil {
		return nil, err
	}
	return GetDeviceInventory200JSONResponse(deviceInventoryToAPI(request.Id, components)), nil
}

// logFetchTimeout bounds how long a raw-log pull waits on the agent.
const logFetchTimeout = 15 * time.Second

// GetDeviceLogs brokers a raw-log pull from the connected agent. The response is bounded,
// redacted, audited and never persisted; reading raw logs requires admin.
func (s *Server) GetDeviceLogs(ctx context.Context, request GetDeviceLogsRequestObject) (GetDeviceLogsResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, GetDeviceLogs403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceLogs404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	ac := s.agents.GetAgent(request.Id)
	if ac == nil {
		s.observeLogPull(logPullOffline, 0)
		return GetDeviceLogs404JSONResponse{Error: "logs not available — device offline"}, nil
	}

	filter := logFilterFromParams(request.Params)
	fetchCtx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()

	start := time.Now()
	entries, total, units, err := ac.RequestLogsSync(fetchCtx, filter)
	s.observeLogPull(logPullResult(err), time.Since(start))
	if err != nil {
		return logsBrokerErrorResponse(err)
	}

	entries = boundLogEntries(entries)
	redactLogEntries(entries)
	// Audit every raw pull: who, which device, and the requested window/filters.
	s.auditLog(ctx, ContextUserID(ctx), "device.logs.read", request.Id.String(), logAuditDetails(filter))
	return GetDeviceLogs200JSONResponse(deviceLogsToAPI(entries, total, units, filter)), nil
}

// observeLogPull records a raw-log broker pull outcome and latency when metrics
// are configured. A zero duration marks a pre-pull outcome (device offline).
func (s *Server) observeLogPull(result string, duration time.Duration) {
	if s.metrics != nil {
		s.metrics.ObserveDeviceLogPull(result, duration)
	}
}

// logPullOffline is the outcome of a pull asked of a device with no connection.
const logPullOffline = metrics.LogPullOffline

// logPullResult classifies a broker outcome into a bounded metric label. The ok
// label is the audited pull count — every ok pull writes one audit event.
func logPullResult(err error) string {
	switch {
	case err == nil:
		return metrics.LogPullOK
	case agentapi.IsCapabilityError(err):
		return metrics.LogPullUnsupported
	case errors.Is(err, agentapi.ErrLogsBusy):
		return metrics.LogPullBusy
	case errors.Is(err, context.DeadlineExceeded):
		return metrics.LogPullTimeout
	default:
		return metrics.LogPullError
	}
}

// logsBrokerErrorResponse maps broker failures to HTTP responses without internals; unsupported,
// busy and timeout conditions are client-visible and the rest are 500.
func logsBrokerErrorResponse(err error) (GetDeviceLogsResponseObject, error) {
	switch {
	case agentapi.IsCapabilityError(err):
		return GetDeviceLogs404JSONResponse{Error: "logs not available"}, nil
	case errors.Is(err, agentapi.ErrLogsBusy):
		return GetDeviceLogs409JSONResponse{Error: "a log request is already in progress for this device"}, nil
	case errors.Is(err, context.DeadlineExceeded):
		return GetDeviceLogs504JSONResponse{Error: "device did not return logs in time"}, nil
	default:
		return nil, err
	}
}

func logFilterFromParams(params GetDeviceLogsParams) device.LogFilter {
	source := ""
	if params.Source != nil {
		source = string(*params.Source)
	}
	return device.LogFilter{
		Level:  derefStr(params.Level),
		From:   derefStr(params.From),
		To:     derefStr(params.To),
		Search: derefStr(params.Search),
		Offset: derefInt(params.Offset, 0),
		Limit:  clampLogLimit(derefInt(params.Limit, defaultLogLimit)),
		Source: source,
		Unit:   derefStr(params.Unit),
	}
}
