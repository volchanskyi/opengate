package agentapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// presenceGrace keeps online a machine that registered after the sweep took its list.
const presenceGrace = time.Minute

// presenceTenant is the tenant a connection's status is written in; an unresolved one is the
// default tenant, as at registration.
func presenceTenant(ac *AgentConn) uuid.UUID {
	if ac.TenantID == uuid.Nil {
		return dbtx.DefaultTenantID
	}
	return ac.TenantID
}

// SweepPresence refreshes last-seen for every connected machine and takes offline each online row
// no connection holds, in one statement per tenant. It returns how many rows went offline.
func (s *AgentServer) SweepPresence(ctx context.Context) (int, error) {
	byTenant := make(map[uuid.UUID][]device.DeviceID)
	s.seenTenants.Range(func(tenant, _ any) bool {
		byTenant[tenant.(uuid.UUID)] = nil
		return true
	})
	s.conns.Range(func(_, conn any) bool {
		ac := conn.(*AgentConn)
		tenant := presenceTenant(ac)
		byTenant[tenant] = append(byTenant[tenant], ac.DeviceID)
		return true
	})

	var offline int
	var failures []error
	for tenant, connected := range byTenant {
		_, n, err := s.devices.RefreshPresence(dbtx.WithTenant(ctx, tenant, false), connected, presenceGrace)
		if err != nil {
			s.logger.Error("presence sweep", "tenant_id", tenant, "error", err)
			failures = append(failures, fmt.Errorf("presence sweep for tenant %s: %w", tenant, err))
			if s.metrics != nil {
				s.metrics.ObservePresenceWriteFailure(appmetrics.PresenceAtSweep)
			}
			continue
		}
		offline += n
	}

	if s.metrics != nil {
		outcome := appmetrics.PresenceSweepOK
		if len(failures) > 0 {
			outcome = appmetrics.PresenceSweepFailed
		}
		s.metrics.ObservePresenceSweep(outcome, offline)
	}
	return offline, errors.Join(failures...)
}
