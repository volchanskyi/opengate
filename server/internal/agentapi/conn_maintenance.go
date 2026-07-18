package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// SendSetMaintenanceMode pushes the desired maintenance state to the agent. It
// is the server-authoritative desired state, delivered whenever an operator
// toggles it and (for a suppressed device) on register to reconcile a
// reconnecting agent. It is ungated — maintenance is a universal operational
// control, not a capability.
func (a *AgentConn) SendSetMaintenanceMode(ctx context.Context, enabled bool) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:    protocol.MsgSetMaintenanceMode,
		Enabled: &enabled,
	})
}

// pushMaintenanceState pushes SetMaintenanceMode(true) when the device is in
// maintenance, so a (re)connecting agent re-enters suppression. Active devices
// need no message — the agent's registration-time default is Active. A read or
// send failure only degrades reconcile, never fails registration, so it is
// logged, not returned.
func (a *AgentConn) pushMaintenanceState(ctx context.Context) {
	d, err := a.devices.Get(ctx, a.DeviceID)
	if err != nil {
		a.logger.Warn("read maintenance state failed", "device_id", a.DeviceID, "error", err)
		return
	}
	if !d.MaintenanceOn {
		return
	}
	if err := a.SendSetMaintenanceMode(ctx, true); err != nil {
		a.logger.Warn("push maintenance state failed", "device_id", a.DeviceID, "error", err)
	}
}

// handleMaintenanceApplied records the maintenance state the agent reported
// applying. The desired state is server-authoritative (Postgres); this is the
// agent's confirmation that it reconciled, kept for observability.
func (a *AgentConn) handleMaintenanceApplied(msg *protocol.ControlMessage) error {
	enabled := msg.Enabled != nil && *msg.Enabled
	a.maintenanceApplied.Store(enabled)
	a.logger.Info("maintenance applied", "device_id", a.DeviceID, "enabled", enabled)
	return nil
}
