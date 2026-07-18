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
	d, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return SetDeviceMaintenance404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}
	if !s.isGroupOwner(ctx, d.GroupID) {
		return SetDeviceMaintenance403JSONResponse{Error: msgForbidden}, nil
	}

	enabled := request.Body.Enabled
	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
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

// GetDeviceMaintenanceSummary implements StrictServerInterface. It returns the
// tenant-scoped count of devices currently in maintenance for the fleet badge.
func (s *Server) GetDeviceMaintenanceSummary(ctx context.Context, _ GetDeviceMaintenanceSummaryRequestObject) (GetDeviceMaintenanceSummaryResponseObject, error) {
	count, err := s.devices.CountInMaintenance(ctx)
	if err != nil {
		return nil, err
	}
	return GetDeviceMaintenanceSummary200JSONResponse{Count: count}, nil
}
