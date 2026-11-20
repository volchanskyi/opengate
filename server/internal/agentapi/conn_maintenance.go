package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// SendSetMaintenanceMode pushes the server-authoritative maintenance state to the agent. It
// requires no capability.
func (a *AgentConn) SendSetMaintenanceMode(ctx context.Context, enabled bool) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:    protocol.MsgSetMaintenanceMode,
		Enabled: &enabled,
	})
}

// pushMaintenanceState tells a machine its sign-in found in maintenance to hold its collectors.
func (a *AgentConn) pushMaintenanceState(ctx context.Context, on bool) {
	if !on {
		return
	}
	if err := a.SendSetMaintenanceMode(ctx, true); err != nil {
		a.logger.Warn("push maintenance state failed", "device_id", a.DeviceID, "error", err)
	}
}

// handleMaintenanceApplied records the maintenance state the agent reports applying; the desired
// state lives in Postgres.
func (a *AgentConn) handleMaintenanceApplied(msg *protocol.ControlMessage) error {
	enabled := msg.Enabled != nil && *msg.Enabled
	a.maintenanceApplied.Store(enabled)
	a.logger.Info("maintenance applied", "device_id", a.DeviceID, "enabled", enabled)
	return nil
}
