package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// RestartDevice implements StrictServerInterface. Restarting an agent is a
// device command, open to every member of the device's organization.
func (s *Server) RestartDevice(ctx context.Context, request RestartDeviceRequestObject) (RestartDeviceResponseObject, error) {
	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return RestartDevice404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	ac := s.agents.GetAgent(request.Id)
	if ac == nil {
		return RestartDevice409JSONResponse{Error: "agent not connected"}, nil
	}

	reason := "restart requested from web UI"
	if request.Body != nil && request.Body.Reason != nil {
		reason = *request.Body.Reason
	}
	reason = sanitizeText(reason, maxReasonLen)

	if err := ac.SendRestartAgent(ctx, reason); err != nil {
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "device.restart", request.Id.String(), reason)
	return RestartDevice200Response{}, nil
}

// UpdateDevice implements StrictServerInterface. Moving a device between groups
// is a configuration change, so the whole endpoint sits behind the admin gate.
func (s *Server) UpdateDevice(ctx context.Context, request UpdateDeviceRequestObject) (UpdateDeviceResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, UpdateDevice403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return UpdateDevice404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	if request.Body.GroupId != nil {
		if resp, err := s.moveDeviceToGroup(ctx, request); resp != nil || err != nil {
			return resp, err
		}
	}

	updated, err := s.devices.Get(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return UpdateDevice200JSONResponse(deviceToAPI(updated)), nil
}

func (s *Server) moveDeviceToGroup(ctx context.Context, request UpdateDeviceRequestObject) (UpdateDeviceResponseObject, error) {
	newGroupID := *request.Body.GroupId
	// The nil UUID is the "no group" destination: it takes the device out of
	// its group instead of moving it into another one, so there is no target
	// group to look up. A named destination must exist in the caller's
	// organization, which the tenant-scoped lookup establishes.
	if newGroupID != uuid.Nil {
		if _, err := s.groups.Get(ctx, newGroupID); err != nil {
			if errors.Is(err, device.ErrGroupNotFound) {
				return UpdateDevice400JSONResponse{Error: "target group not found"}, nil
			}
			return nil, err
		}
	}
	if err := s.devices.UpdateGroup(ctx, request.Id, newGroupID); err != nil {
		return nil, err
	}
	return nil, nil
}

// DeleteDevice implements StrictServerInterface. Removing a device from the
// fleet — and purging its telemetry with it — is a configuration change behind
// the admin gate.
func (s *Server) DeleteDevice(ctx context.Context, request DeleteDeviceRequestObject) (DeleteDeviceResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, DeleteDevice403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if err := s.requireDeviceInScope(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return DeleteDevice404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	if err := s.purgeDeletedDevice(ctx, request.Id); err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "device.delete", request.Id.String(), "")
	return DeleteDevice204Response{}, nil
}

// purgeDeletedDevice erases a deleted device's centralized telemetry across
// every store via the lifecycle orchestrator: it tombstones the device
// (blocking further ingest), deprovisions the agent, deletes its VictoriaMetrics
// series and Postgres rows, and verifies emptiness. Without a wired purger it
// falls back to the plain Postgres delete plus agent deregistration.
func (s *Server) purgeDeletedDevice(ctx context.Context, deviceID uuid.UUID) error {
	if s.purger == nil {
		if err := s.devices.Delete(ctx, deviceID); err != nil {
			return err
		}
		s.agents.DeregisterAgent(ctx, deviceID)
		return nil
	}
	claims := ContextClaims(ctx)
	if claims == nil {
		return errors.New("missing tenant claims")
	}
	userID := ContextUserID(ctx)
	job, err := s.purger.PurgeDevice(ctx, claims.OrgID, deviceID, &userID)
	if err != nil {
		return err
	}
	// A device purge is fast (VM delete issued, Postgres rows removed, bounded
	// emptiness verify); run it in-request so the device is gone on return. A
	// still-pending VM compaction leaves the job resumable for the sweep.
	return s.purger.Run(ctx, job)
}
