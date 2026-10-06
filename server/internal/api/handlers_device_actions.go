package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// defaultRestartReason is recorded when a caller states none, so the agent frame always
// carries a decodable reason.
const defaultRestartReason = "restart requested from web UI"

// RestartDevice implements StrictServerInterface. Restarting an agent is a
// device command, open to every member of the device's tenant.
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

	// A reason without printable text encodes a frame the agent cannot decode, so it is refused;
	// an omitted reason keeps the server default.
	reason := defaultRestartReason
	if request.Body != nil && request.Body.Reason != nil {
		reason = *request.Body.Reason
	}
	if strings.TrimSpace(reason) == "" {
		return RestartDevice400JSONResponse{Error: "reason must not be empty"}, nil
	}
	if msg := invalidText("reason", reason, maxReasonLen); msg != "" {
		return RestartDevice400JSONResponse{Error: msg}, nil
	}

	if err := ac.SendRestartAgent(ctx, reason); err != nil {
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "device.restart", request.Id.String(), reason)
	return RestartDevice200Response{}, nil
}

// UpdateDevice implements StrictServerInterface. Moving a device between sites
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

	if request.Body.SiteId != nil {
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
	newGroupID := *request.Body.SiteId
	// The nil UUID is the "no site" destination and needs no lookup; a named destination must
	// exist in the caller's tenant.
	if newGroupID != uuid.Nil {
		if _, err := s.sites.Get(ctx, newGroupID); err != nil {
			if errors.Is(err, device.ErrSiteNotFound) {
				return UpdateDevice400JSONResponse{Error: "target site not found"}, nil
			}
			return nil, err
		}
	}
	if err := s.devices.UpdateSite(ctx, request.Id, newGroupID); err != nil {
		return nil, err
	}
	return nil, nil
}

// DeleteDevice implements StrictServerInterface; deleting a device and purging its telemetry
// requires admin.
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
	// Delete and erasure are one act, so a missing purger refuses the delete. Scope is checked
	// first, so another tenant's device answers 404 and leaks nothing about the wiring.
	if s.purger == nil {
		return DeleteDevice403JSONResponse{Error: msgPurgeNotConfigured}, nil
	}

	if err := s.purgeDeletedDevice(ctx, request.Id); err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "device.delete", request.Id.String(), "")
	return DeleteDevice204Response{}, nil
}

// purgeDeletedDevice erases a deleted device's telemetry from every store through the lifecycle
// orchestrator: tombstone, deprovision, delete series and rows, verify emptiness.
func (s *Server) purgeDeletedDevice(ctx context.Context, deviceID uuid.UUID) error {
	claims := ContextClaims(ctx)
	if claims == nil {
		return errors.New("missing tenant claims")
	}
	userID := ContextUserID(ctx)
	job, err := s.purger.PurgeDevice(ctx, claims.TenantID, deviceID, &userID)
	if err != nil {
		return err
	}
	// The purge is fast, so it runs in-request and the device is gone on return; a pending
	// compaction leaves the job resumable for the sweep.
	return s.purger.Run(ctx, job)
}
