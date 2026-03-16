package api

import (
	"context"
	"fmt"
	"time"

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
	if !isAdmin(ctx) {
		return PublishUpdate403JSONResponse{Error: "admin access required"}, nil
	}
	if s.signing == nil || s.manifests == nil {
		return PublishUpdate403JSONResponse{Error: "update system not configured"}, nil
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
	if !isAdmin(ctx) {
		return PushUpdate403JSONResponse{Error: "admin access required"}, nil
	}
	if s.signing == nil || s.manifests == nil {
		return PushUpdate403JSONResponse{Error: "update system not configured"}, nil
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

	agents := s.agents.ListConnectedAgents()
	pushed := 0
	for _, agent := range agents {
		if agent.OS != request.Body.Os || agent.Arch != request.Body.Arch {
			continue
		}
		if agent.AgentVersion == m.Version {
			continue // already up to date
		}
		if request.Body.DeviceIds != nil {
			found := false
			for _, id := range *request.Body.DeviceIds {
				if id == agent.DeviceID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if err := agent.SendAgentUpdate(ctx, m.Version, m.URL, m.Signature); err != nil {
			s.logger.Warn("push update to agent failed",
				"device_id", agent.DeviceID,
				"error", err)
			continue
		}
		pushed++
	}

	s.auditLog(ContextUserID(ctx), "update.push",
		fmt.Sprintf("%s/%s", request.Body.Os, request.Body.Arch),
		fmt.Sprintf("version=%s pushed=%d", m.Version, pushed))

	return PushUpdate200JSONResponse{PushedCount: pushed}, nil
}

// GetUpdateSigningKey implements StrictServerInterface.
func (s *Server) GetUpdateSigningKey(ctx context.Context, _ GetUpdateSigningKeyRequestObject) (GetUpdateSigningKeyResponseObject, error) {
	if !isAdmin(ctx) {
		return GetUpdateSigningKey403JSONResponse{Error: "admin access required"}, nil
	}
	if s.signing == nil {
		return GetUpdateSigningKey403JSONResponse{Error: "update system not configured"}, nil
	}

	return GetUpdateSigningKey200JSONResponse{PublicKey: s.signing.PublicKeyHex()}, nil
}
