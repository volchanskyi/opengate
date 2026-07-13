package agentapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// tombstoneLoader warms the in-memory deny-list from the persisted deleted-ids
// store at startup. *lifecycle.TombstoneStore satisfies it.
type tombstoneLoader interface {
	ListAll(ctx context.Context) ([]lifecycle.Tombstone, error)
}

// WarmTombstones loads the persisted deny-list into the in-memory cache so a
// device purged before this process started stays rejected on reconnect. It is
// a no-op when no persisted store is wired. Call it once at startup, before
// serving.
func (s *AgentServer) WarmTombstones(ctx context.Context) error {
	if s.tombstoneStore == nil {
		return nil
	}
	tombstones, err := s.tombstoneStore.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("warm tombstones: %w", err)
	}
	for _, tomb := range tombstones {
		if tomb.DeviceID != nil {
			s.tombstones.Store(*tomb.DeviceID, struct{}{})
		}
	}
	return nil
}

// DeregisterAgent marks a device as deleted and notifies the connected agent
// (if online) to clean up and exit. Future reconnection attempts will be rejected.
func (s *AgentServer) DeregisterAgent(ctx context.Context, deviceID protocol.DeviceID) {
	s.tombstones.Store(deviceID, struct{}{})

	ac := s.GetAgent(deviceID)
	if ac == nil {
		return
	}

	if err := ac.SendAgentDeregistered(ctx, "device deleted by administrator"); err != nil {
		s.logger.Error("send deregistered to agent", "error", err, "device_id", deviceID)
	}

	// Close connection so the control loop exits.
	if err := ac.Close(); err != nil {
		s.logger.Warn("close agent connection on deregister", "error", err, "device_id", deviceID)
	}
}

// DeregisterOrg tombstones and disconnects every connected agent in an org, for
// a tenant-wide purge. Offline agents in the org are covered by the persisted
// per-device deny-list entries the purge records, so they are rejected by their
// own id when they next reconnect.
func (s *AgentServer) DeregisterOrg(ctx context.Context, orgID uuid.UUID) {
	for _, ac := range s.ListConnectedAgents() {
		if ac.OrgID == orgID {
			s.DeregisterAgent(ctx, ac.DeviceID)
		}
	}
}
