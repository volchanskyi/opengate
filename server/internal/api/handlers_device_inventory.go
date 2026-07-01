package api

import (
	"context"
	"errors"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// GetDeviceHardware implements StrictServerInterface.
func (s *Server) GetDeviceHardware(ctx context.Context, request GetDeviceHardwareRequestObject) (GetDeviceHardwareResponseObject, error) {
	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceHardware404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, d.GroupID) {
		return GetDeviceHardware403JSONResponse{Error: msgForbidden}, nil
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

// GetDeviceLogs implements StrictServerInterface.
func (s *Server) GetDeviceLogs(ctx context.Context, request GetDeviceLogsRequestObject) (GetDeviceLogsResponseObject, error) {
	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetDeviceLogs404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, d.GroupID) {
		return GetDeviceLogs403JSONResponse{Error: msgForbidden}, nil
	}

	filter := logFilterFromParams(request.Params)
	hasRecent, err := s.deviceLogs.HasRecent(ctx, request.Id, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	if hasRecent && !boolParam(request.Params.Refresh) {
		return s.cachedLogsResponse(ctx, request.Id, filter, "logs not available")
	}
	return s.refreshLogsFromAgent(ctx, request.Id, filter)
}

func logFilterFromParams(params GetDeviceLogsParams) device.LogFilter {
	return device.LogFilter{
		Level:  derefStr(params.Level),
		From:   derefStr(params.From),
		To:     derefStr(params.To),
		Search: derefStr(params.Search),
		Offset: derefInt(params.Offset, 0),
		Limit:  derefInt(params.Limit, 300),
	}
}

func boolParam(v *bool) bool {
	return v != nil && *v
}

func (s *Server) cachedLogsResponse(ctx context.Context, id device.DeviceID, filter device.LogFilter, missing string) (GetDeviceLogsResponseObject, error) {
	entries, total, err := s.deviceLogs.Query(ctx, id, filter)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return GetDeviceLogs404JSONResponse{Error: missing}, nil
	}
	return GetDeviceLogs200JSONResponse(deviceLogsToAPI(entries, total, filter)), nil
}

func (s *Server) refreshLogsFromAgent(ctx context.Context, id device.DeviceID, filter device.LogFilter) (GetDeviceLogsResponseObject, error) {
	ac := s.agents.GetAgent(id)
	if ac == nil {
		return s.cachedLogsResponse(ctx, id, filter, "logs not available — device offline")
	}
	if err := ac.SendRequestDeviceLogs(ctx, device.LogFilter{Limit: 1000}); err != nil {
		if agentapi.IsCapabilityError(err) {
			return GetDeviceLogs404JSONResponse{Error: "logs not available"}, nil
		}
		return nil, err
	}
	return GetDeviceLogs202Response{}, nil
}
