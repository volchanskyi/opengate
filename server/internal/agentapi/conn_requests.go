package agentapi

import (
	"context"
	"math"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// SendSessionRequest sends a SessionRequest naming the relay address and the host's helpers; the
// agent requires a token and a relay URL, so an empty one is refused.
func (a *AgentConn) SendSessionRequest(ctx context.Context, token protocol.SessionToken, relayURL string, perms protocol.Permissions, helpers []string) error {
	if err := requireNonEmptyFields(protocol.MsgSessionRequest,
		controlField{"token", string(token)},
		controlField{"relay_url", relayURL},
	); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:        protocol.MsgSessionRequest,
		Token:       token,
		RelayURL:    relayURL,
		Permissions: &perms,
		IceServers:  helpers,
	})
}

// SendAgentUpdate sends an AgentUpdate to the agent; version, url and signature are required,
// and the updater checks sha256 against the downloaded artifact.
func (a *AgentConn) SendAgentUpdate(ctx context.Context, version, url, sha256, signature string) error {
	if err := requireNonEmptyFields(protocol.MsgAgentUpdate,
		controlField{"version", version},
		controlField{"url", url},
		controlField{"signature", signature},
	); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:      protocol.MsgAgentUpdate,
		Version:   version,
		URL:       url,
		SHA256:    sha256,
		Signature: signature,
	})
}

// SendAgentDeregistered tells the agent its device was deleted and it should clean up.
func (a *AgentConn) SendAgentDeregistered(ctx context.Context, reason string) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgAgentDeregistered,
		Reason: reason,
	})
}

// SendRestartAgent asks the agent to restart itself.
func (a *AgentConn) SendRestartAgent(ctx context.Context, reason string) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgRestartAgent,
		Reason: reason,
	})
}

// SendRequestHardwareReport asks the agent to collect and send hardware info.
func (a *AgentConn) SendRequestHardwareReport(ctx context.Context) error {
	if err := a.requireCapability(protocol.CapHardwareInventory); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type: protocol.MsgRequestHardwareReport,
	})
}

// SendRequestHealthWindow asks the agent for its bounded recent health summary window.
func (a *AgentConn) SendRequestHealthWindow(ctx context.Context, sinceTS int64, limit uint32) error {
	if err := a.requireCapability(protocol.CapHealthWindow); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:    protocol.MsgRequestHealthWindow,
		SinceTS: sinceTS,
		Limit:   limit,
	})
}

// SendPushAlertRules pushes a ruleset and the per-machine alert ceiling to the agent; a zero
// ceiling leaves the agent's allowance unchanged. Requires the ThresholdAlerts capability.
func (a *AgentConn) SendPushAlertRules(ctx context.Context, ruleset RuleSet) error {
	if err := a.requireCapability(protocol.CapThresholdAlerts); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:                protocol.MsgPushAlertRules,
		AlertRules:          ruleset.Rules,
		DeviceHourlyCeiling: ruleset.DeviceHourlyCeiling,
	})
}

// SendRequestLocalHistory asks the agent for a bounded, full-resolution slice of
// one dimension's local history. Gated by the Backfill capability.
func (a *AgentConn) SendRequestLocalHistory(ctx context.Context, dim string, fromTS, toTS int64, maxPoints uint32) error {
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:      protocol.MsgRequestLocalHistory,
		Dim:       dim,
		FromTS:    fromTS,
		ToTS:      toTS,
		MaxPoints: maxPoints,
	})
}

// SendRequestDeviceLogs asks the agent to collect and send filtered log entries.
func (a *AgentConn) SendRequestDeviceLogs(ctx context.Context, filter device.LogFilter) error {
	if err := a.requireCapability(protocol.CapDeviceLogs); err != nil {
		return err
	}
	offset := clampNonNegativeUint32(filter.Offset)
	limit := clampNonNegativeUint32(filter.Limit)
	return a.sendControl(&protocol.ControlMessage{
		Type:      protocol.MsgRequestDeviceLogs,
		LogLevel:  filter.Level,
		TimeFrom:  filter.From,
		TimeTo:    filter.To,
		Search:    filter.Search,
		LogOffset: offset,
		LogLimit:  limit,
		Source:    filter.Source,
		Unit:      filter.Unit,
	})
}

// clampNonNegativeUint32 narrows a non-negative int to uint32, clamping any
// value outside [0, math.MaxUint32] to the boundary. Negative values become 0.
func clampNonNegativeUint32(v int) uint32 {
	if v <= 0 {
		return 0
	}
	if uint64(v) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}
