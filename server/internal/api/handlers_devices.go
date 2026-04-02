package api

import (
	"context"
	"errors"
	"time"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// RestartDevice implements StrictServerInterface.
func (s *Server) RestartDevice(ctx context.Context, request RestartDeviceRequestObject) (RestartDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return RestartDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return RestartDevice403JSONResponse{Error: msgForbidden}, nil
	}

	ac := s.agents.GetAgent(request.Id)
	if ac == nil {
		return RestartDevice409JSONResponse{Error: "agent not connected"}, nil
	}

	reason := "restart requested from web UI"
	if request.Body != nil && request.Body.Reason != nil {
		reason = *request.Body.Reason
	}

	if err := ac.SendRestartAgent(ctx, reason); err != nil {
		return nil, err
	}

	s.auditLog(ContextUserID(ctx), "device.restart", request.Id.String(), reason)

	return RestartDevice200Response{}, nil
}

// GetDeviceHardware implements StrictServerInterface.
func (s *Server) GetDeviceHardware(ctx context.Context, request GetDeviceHardwareRequestObject) (GetDeviceHardwareResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetDeviceHardware404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return GetDeviceHardware403JSONResponse{Error: msgForbidden}, nil
	}

	hw, err := s.store.GetDeviceHardware(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// No cached data — request from agent if online
			ac := s.agents.GetAgent(request.Id)
			if ac == nil {
				return GetDeviceHardware404JSONResponse{Error: "hardware info not available"}, nil
			}
			if err := ac.SendRequestHardwareReport(ctx); err != nil {
				return nil, err
			}
			return GetDeviceHardware202Response{}, nil
		}
		return nil, err
	}

	return GetDeviceHardware200JSONResponse(deviceHardwareToAPI(hw)), nil
}

// GetDeviceLogs implements StrictServerInterface.
func (s *Server) GetDeviceLogs(ctx context.Context, request GetDeviceLogsRequestObject) (GetDeviceLogsResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetDeviceLogs404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return GetDeviceLogs403JSONResponse{Error: msgForbidden}, nil
	}

	filter := db.LogFilter{
		Level:  derefStr(request.Params.Level),
		From:   derefStr(request.Params.From),
		To:     derefStr(request.Params.To),
		Search: derefStr(request.Params.Search),
		Offset: derefInt(request.Params.Offset, 0),
		Limit:  derefInt(request.Params.Limit, 100),
	}

	// Check if we have recent cached logs (5-minute TTL)
	hasRecent, err := s.store.HasRecentLogs(ctx, request.Id, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	if hasRecent {
		entries, total, err := s.store.QueryDeviceLogs(ctx, request.Id, filter)
		if err != nil {
			return nil, err
		}
		return GetDeviceLogs200JSONResponse(deviceLogsToAPI(entries, total, filter)), nil
	}

	// No recent cache — request from agent if online
	ac := s.agents.GetAgent(request.Id)
	if ac == nil {
		// Agent offline — try serving stale cache if any exists
		entries, total, err := s.store.QueryDeviceLogs(ctx, request.Id, filter)
		if err != nil || total == 0 {
			return GetDeviceLogs404JSONResponse{Error: "logs not available — device offline"}, nil
		}
		return GetDeviceLogs200JSONResponse(deviceLogsToAPI(entries, total, filter)), nil
	}

	if err := ac.SendRequestDeviceLogs(ctx, filter); err != nil {
		return nil, err
	}
	return GetDeviceLogs202Response{}, nil
}

// ListDevices implements StrictServerInterface.
func (s *Server) ListDevices(ctx context.Context, request ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	var devices []*db.Device
	var err error
	if request.Params.GroupId != nil {
		if !s.isGroupOwner(ctx, *request.Params.GroupId) {
			return ListDevices403JSONResponse{Error: msgForbidden}, nil
		}
		devices, err = s.store.ListDevices(ctx, *request.Params.GroupId)
	} else if isAdmin(ctx) {
		devices, err = s.store.ListAllDevices(ctx)
	} else {
		devices, err = s.store.ListDevicesForOwner(ctx, ContextUserID(ctx))
	}
	if err != nil {
		return nil, err
	}

	return ListDevices200JSONResponse(devicesToAPI(devices)), nil
}

// GetDevice implements StrictServerInterface.
func (s *Server) GetDevice(ctx context.Context, request GetDeviceRequestObject) (GetDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return GetDevice403JSONResponse{Error: msgForbidden}, nil
	}

	return GetDevice200JSONResponse(deviceToAPI(device)), nil
}

// UpdateDevice implements StrictServerInterface.
func (s *Server) UpdateDevice(ctx context.Context, request UpdateDeviceRequestObject) (UpdateDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return UpdateDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return UpdateDevice403JSONResponse{Error: msgForbidden}, nil
	}

	if request.Body.GroupId != nil {
		newGroupID := *request.Body.GroupId
		// Verify the target group exists and the user owns it.
		if _, err := s.store.GetGroup(ctx, newGroupID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return UpdateDevice400JSONResponse{Error: "target group not found"}, nil
			}
			return nil, err
		}
		if !s.isGroupOwner(ctx, newGroupID) {
			return UpdateDevice403JSONResponse{Error: msgForbidden}, nil
		}
		if err := s.store.UpdateDeviceGroup(ctx, request.Id, newGroupID); err != nil {
			return nil, err
		}
	}

	updated, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		return nil, err
	}

	return UpdateDevice200JSONResponse(deviceToAPI(updated)), nil
}

// DeleteDevice implements StrictServerInterface.
func (s *Server) DeleteDevice(ctx context.Context, request DeleteDeviceRequestObject) (DeleteDeviceResponseObject, error) {
	device, err := s.store.GetDevice(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return DeleteDevice404JSONResponse{Error: "device not found"}, nil
		}
		return nil, err
	}

	if !s.isGroupOwner(ctx, device.GroupID) {
		return DeleteDevice403JSONResponse{Error: msgForbidden}, nil
	}

	if err := s.store.DeleteDevice(ctx, request.Id); err != nil {
		return nil, err
	}

	// Notify connected agent to clean up and reject future reconnections.
	s.agents.DeregisterAgent(ctx, request.Id)

	s.auditLog(ContextUserID(ctx), "device.delete", request.Id.String(), "")

	return DeleteDevice204Response{}, nil
}
