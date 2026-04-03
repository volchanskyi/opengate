package api

import (
	"context"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/osutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// ListUpdateManifests implements StrictServerInterface.
func (s *Server) ListUpdateManifests(ctx context.Context, _ ListUpdateManifestsRequestObject) (ListUpdateManifestsResponseObject, error) {
	if s.manifests == nil {
		return ListUpdateManifests200JSONResponse([]AgentManifest{}), nil
	}

	list, err := s.manifests.List(ctx)
	if err != nil {
		return nil, err
	}
	if list == nil {
		return ListUpdateManifests200JSONResponse([]AgentManifest{}), nil
	}

	return ListUpdateManifests200JSONResponse(manifestsToAPI(list)), nil
}

// PublishUpdate implements StrictServerInterface.
func (s *Server) PublishUpdate(ctx context.Context, request PublishUpdateRequestObject) (PublishUpdateResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PublishUpdate403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.signing == nil || s.manifests == nil {
		return PublishUpdate403JSONResponse{Error: msgUpdateNotConfigured}, nil
	}

	sig, err := s.signing.SignHash(request.Body.Sha256)
	if err != nil {
		return nil, fmt.Errorf("sign hash: %w", err)
	}

	m := &updater.Manifest{
		Version:   request.Body.Version,
		OS:        request.Body.Os,
		Arch:      request.Body.Arch,
		URL:       request.Body.Url,
		SHA256:    request.Body.Sha256,
		Signature: sig,
		CreatedAt: time.Now().UTC(),
	}

	if err := s.manifests.Put(ctx, m); err != nil {
		return nil, fmt.Errorf("store manifest: %w", err)
	}

	s.auditLog(ContextUserID(ctx), "update.publish",
		fmt.Sprintf("%s/%s", m.OS, m.Arch),
		fmt.Sprintf("version=%s", m.Version))

	return PublishUpdate200JSONResponse(manifestToAPI(m)), nil
}

// PushUpdate implements StrictServerInterface.
func (s *Server) PushUpdate(ctx context.Context, request PushUpdateRequestObject) (PushUpdateResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PushUpdate403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.signing == nil || s.manifests == nil {
		return PushUpdate403JSONResponse{Error: msgUpdateNotConfigured}, nil
	}

	m, err := s.manifests.Get(ctx, request.Body.Os, request.Body.Arch)
	if err != nil {
		return nil, fmt.Errorf("get manifest: %w", err)
	}
	if m == nil {
		return PushUpdate404JSONResponse{Error: fmt.Sprintf("no manifest for %s/%s", request.Body.Os, request.Body.Arch)}, nil
	}
	if m.Version != request.Body.Version {
		return PushUpdate404JSONResponse{Error: fmt.Sprintf("manifest version %s does not match requested %s", m.Version, request.Body.Version)}, nil
	}

	// Build a set for O(1) device ID lookups when filtering is requested.
	var targetSet map[string]struct{}
	if request.Body.DeviceIds != nil {
		targetSet = make(map[string]struct{}, len(*request.Body.DeviceIds))
		for _, id := range *request.Body.DeviceIds {
			targetSet[id.String()] = struct{}{}
		}
	}

	eligible := s.eligibleAgents(request.Body.Os, request.Body.Arch, m.Version, targetSet)
	pushed := 0
	for _, agent := range eligible {
		if err := agent.SendAgentUpdate(ctx, m.Version, m.URL, m.SHA256, m.Signature); err != nil {
			s.logger.Warn("push update to agent failed",
				"device_id", agent.DeviceID,
				"error", err)
			continue
		}
		pushed++

		// Record pending update status for tracking.
		du := &db.DeviceUpdate{
			DeviceID: agent.DeviceID,
			Version:  m.Version,
			Status:   db.UpdateStatusPending,
		}
		if err := s.store.CreateDeviceUpdate(ctx, du); err != nil {
			s.logger.Warn("record device update failed",
				"device_id", agent.DeviceID,
				"error", err)
		}
	}

	s.auditLog(ContextUserID(ctx), "update.push",
		fmt.Sprintf("%s/%s", request.Body.Os, request.Body.Arch),
		fmt.Sprintf("version=%s pushed=%d", m.Version, pushed))

	return PushUpdate200JSONResponse{PushedCount: pushed}, nil
}

// eligibleAgents returns connected agents that match os/arch, are not already
// on the target version, and (optionally) belong to the target device ID set.
func (s *Server) eligibleAgents(osName, arch, version string, targetSet map[string]struct{}) []*agentapi.AgentConn {
	var eligible []*agentapi.AgentConn
	for _, agent := range s.agents.ListConnectedAgents() {
		if osutil.NormalizeOS(agent.OS) != osName || osutil.NormalizeArch(agent.Arch) != arch {
			continue
		}
		if agent.AgentVersion == version {
			continue
		}
		if targetSet != nil {
			if _, ok := targetSet[agent.DeviceID.String()]; !ok {
				continue
			}
		}
		eligible = append(eligible, agent)
	}
	return eligible
}

// GetUpdateStatus implements StrictServerInterface.
func (s *Server) GetUpdateStatus(ctx context.Context, request GetUpdateStatusRequestObject) (GetUpdateStatusResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, GetUpdateStatus403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	updates, err := s.store.ListDeviceUpdatesByVersion(ctx, request.Version)
	if err != nil {
		return nil, fmt.Errorf("list device updates: %w", err)
	}

	result := make([]DeviceUpdate, 0, len(updates))
	for _, du := range updates {
		item := DeviceUpdate{
			Id:       du.ID,
			DeviceId: du.DeviceID,
			Version:  du.Version,
			Status:   DeviceUpdateStatus(du.Status),
			Error:    du.Error,
			PushedAt: du.PushedAt,
		}
		if du.AckedAt != nil {
			item.AckedAt = du.AckedAt
		}
		result = append(result, item)
	}

	return GetUpdateStatus200JSONResponse(result), nil
}

// GetUpdateSigningKey implements StrictServerInterface.
func (s *Server) GetUpdateSigningKey(ctx context.Context, _ GetUpdateSigningKeyRequestObject) (GetUpdateSigningKeyResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, GetUpdateSigningKey403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.signing == nil {
		return GetUpdateSigningKey403JSONResponse{Error: msgUpdateNotConfigured}, nil
	}

	return GetUpdateSigningKey200JSONResponse{PublicKey: s.signing.PublicKeyHex()}, nil
}
