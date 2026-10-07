package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// SetDeviceMaintenance implements StrictServerInterface by persisting the desired suppression
// state and pushing it; it succeeds with the agent offline and reconciles on the next connect.
func (s *Server) SetDeviceMaintenance(ctx context.Context, request SetDeviceMaintenanceRequestObject) (SetDeviceMaintenanceResponseObject, error) {
	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return SetDeviceMaintenance404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	enabled := request.Body.Enabled
	reason := ""
	if request.Body.Reason != nil {
		reason = sanitizeText(*request.Body.Reason, maxReasonLen)
	}
	userID := ContextUserID(ctx)

	if err := s.devices.SetMaintenance(ctx, request.Id, enabled, userID, reason); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return SetDeviceMaintenance404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	s.pushMaintenanceToAgent(ctx, request.Id, enabled)

	action := "device.maintenance.exit"
	if enabled {
		action = "device.maintenance.enter"
	}
	s.auditLog(ctx, userID, action, request.Id.String(), reason)

	updated, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return SetDeviceMaintenance200JSONResponse(deviceToAPI(updated)), nil
}

// pushMaintenanceToAgent delivers the desired state best-effort; a missing agent or failed push
// leaves the persisted toggle intact.
func (s *Server) pushMaintenanceToAgent(ctx context.Context, deviceID uuid.UUID, enabled bool) {
	ac := s.agents.GetAgent(deviceID)
	if ac == nil {
		return
	}
	if err := ac.SendSetMaintenanceMode(ctx, enabled); err != nil {
		s.logger.Warn("push maintenance mode failed", "device_id", deviceID, "error", err)
	}
}
