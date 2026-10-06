package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// maxDiscoveryPayloadBytes bounds a DiscoveryReport; a full package inventory is far larger
	// than a numeric-telemetry message.
	maxDiscoveryPayloadBytes = 1 << 20
	// minDiscoveryIntervalSeconds throttles reports from one connection; the agent only reports
	// on a profile change.
	minDiscoveryIntervalSeconds = 30
)

// handleDiscoveryReport persists a discovery report as the device's inventory footprint under the
// connection's authoritative tenant, never an agent-supplied one.
func (a *AgentConn) handleDiscoveryReport(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.inventory == nil || !a.acceptDiscovery(msg.TS, payloadLen) {
		return nil
	}
	components := discoveryComponents(msg)
	if len(components) == 0 {
		a.dropTelemetry("empty_discovery", "type", protocol.MsgDiscoveryReport)
		return nil
	}
	ts := a.telemetryTimestamp(msg.TS)
	a.persistTelemetry(ctx, 1, func(jobCtx context.Context, _ dbtx.Tenant) error {
		return a.inventory.Replace(jobCtx, a.DeviceID, ts, components)
	})
	return nil
}

// acceptDiscovery enforces the discovery payload cap and the per-connection interval floor.
func (a *AgentConn) acceptDiscovery(ts int64, payloadLen int) bool {
	if payloadLen > maxDiscoveryPayloadBytes {
		a.dropTelemetry("discovery_payload_too_large", "bytes", payloadLen)
		return false
	}
	if ts > 0 {
		if a.telemetryLast == nil {
			a.telemetryLast = make(map[protocol.ControlMessageType]int64)
		}
		if last, ok := a.telemetryLast[protocol.MsgDiscoveryReport]; ok && ts-last < minDiscoveryIntervalSeconds {
			a.dropTelemetry("discovery_interval_floor", "ts", ts, "last_ts", last)
			return false
		}
		a.telemetryLast[protocol.MsgDiscoveryReport] = ts
	}
	return a.acceptedTelemetry(protocol.MsgDiscoveryReport)
}

// discoveryComponents flattens the DiscoveryReport categories into inventory components, naming
// each by its owning process, unit, engine, container or package.
func discoveryComponents(msg *protocol.ControlMessage) []inventory.Component {
	out := make([]inventory.Component, 0,
		len(msg.Ports)+len(msg.Services)+len(msg.DBEngines)+len(msg.Containers)+len(msg.Packages))
	for _, p := range msg.Ports {
		out = append(out, inventory.Component{Kind: inventory.KindPort, Name: p.Process, Proto: p.Proto, Port: p.Port})
	}
	for _, s := range msg.Services {
		out = append(out, inventory.Component{Kind: inventory.KindService, Name: s.Name, State: s.State})
	}
	for _, e := range msg.DBEngines {
		out = append(out, inventory.Component{Kind: inventory.KindDBEngine, Name: e.Engine, Version: e.Version, Port: e.Port})
	}
	for _, c := range msg.Containers {
		out = append(out, inventory.Component{Kind: inventory.KindContainer, Name: c.Name, Runtime: c.Runtime, Image: c.Image, State: c.State})
	}
	for _, pk := range msg.Packages {
		out = append(out, inventory.Component{Kind: inventory.KindPackage, Name: pk.Name, Version: pk.Version})
	}
	return out
}
