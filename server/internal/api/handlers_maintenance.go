package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// SetDeviceMaintenance implements StrictServerInterface. Maintenance is the
// server-authoritative desired suppression state: it is persisted and pushed to
// the agent, but because it is a desired state rather than a live command it
// succeeds even when the agent is offline (reconciled on the next connect), so
// there is no "agent not connected" failure like RestartDevice has.
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

// pushMaintenanceToAgent delivers the new desired state to a connected agent.
// An offline agent reconciles on its next register, so a missing agent — and a
// best-effort push failure — are both non-fatal to the persisted toggle.
func (s *Server) pushMaintenanceToAgent(ctx context.Context, deviceID uuid.UUID, enabled bool) {
	ac := s.agents.GetAgent(deviceID)
	if ac == nil {
		return
	}
	if err := ac.SendSetMaintenanceMode(ctx, enabled); err != nil {
		s.logger.Warn("push maintenance mode failed", "device_id", deviceID, "error", err)
	}
}
