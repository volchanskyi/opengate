package agentapi

import (
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// handleRequestBackfillSlot admits or defers a reconnect-backfill drain through
// the server-coordinated scheduler and replies to the agent with the decision
// (GrantBackfill with a rate + deadline, or DeferBackfill with a retry-after).
//
// The org is taken from the authenticated connection, never from the agent's
// message, so backfill admission is always scoped to the right tenant. A
// connection without a scheduler (test/programmatic) or an agent that never
// advertised the Backfill capability is a silent no-op — the agent falls back
// to holding its durable data and retrying.
func (a *AgentConn) handleRequestBackfillSlot(msg *protocol.ControlMessage) error {
	if a.scheduler == nil {
		return nil
	}
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		a.logger.Debug("ignoring backfill slot request: capability not advertised", "device_id", a.DeviceID)
		return nil
	}

	decision := a.scheduler.RequestSlot(a.DeviceID, a.OrgID, SlotRequest{
		PendingSamples: msg.PendingSamples,
		OldestTS:       msg.OldestTS,
	})
	if decision.Grant {
		return a.sendControl(&protocol.ControlMessage{
			Type:     protocol.MsgGrantBackfill,
			Rate:     decision.Rate,
			Deadline: decision.Deadline,
		})
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:       protocol.MsgDeferBackfill,
		RetryAfter: decision.RetryAfter,
	})
}
