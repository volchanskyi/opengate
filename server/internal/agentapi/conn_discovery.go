package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// maxDiscoveryPayloadBytes bounds a single DiscoveryReport. It is far larger
	// than the numeric-telemetry cap because a full package inventory (up to the
	// agent's MAX_PACKAGES) is legitimately hundreds of KiB; a report beyond this
	// ceiling is dropped rather than persisted.
	maxDiscoveryPayloadBytes = 1 << 20 // 1 MiB
	// minDiscoveryIntervalSeconds throttles reports from one connection. The
	// agent only emits on a profile change, so a steady host is silent; this
	// bounds a misbehaving agent without dropping a legitimate change report.
	minDiscoveryIntervalSeconds = 30
)

// handleDiscoveryReport persists a WS-16 auto-discovery report as the device's
// current inventory footprint, scoped to the connection's authoritative org
// (never the agent-supplied org). The report is descriptive attack-surface data
// only; nothing here becomes a VictoriaMetrics label.
func (a *AgentConn) handleDiscoveryReport(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.inventory == nil || !a.acceptDiscovery(msg.TS, payloadLen) {
		return nil
	}
	components := discoveryComponents(msg)
	if len(components) == 0 {
		return nil
	}
	ts := telemetryTimestamp(msg.TS)
	a.persistTelemetry(ctx, func(jobCtx context.Context, _ dbtx.Tenant) error {
		return a.inventory.Replace(jobCtx, a.DeviceID, ts, components)
	})
	return nil
}

// acceptDiscovery enforces the discovery payload cap and per-connection interval
// floor, mirroring acceptTelemetry but with discovery-appropriate bounds.
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

// discoveryComponents flattens the five DiscoveryReport categories into inventory
// components. Name is each component's primary label: the owning process for a
// port, the unit for a service, the engine for a DB engine, and the container or
// package name otherwise.
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
