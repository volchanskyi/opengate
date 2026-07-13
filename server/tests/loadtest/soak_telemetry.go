package main

import (
	"fmt"
	"io"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// tenantAgent is one deterministic (org, agent) slot in an N-tenant × M-agent
// soak plan: a stable org index and a tenant-tagged hostname.
type tenantAgent struct {
	orgIndex   int
	agentIndex int
	hostname   string
}

// buildTenantAgents partitions tenants × perTenant agents deterministically so a
// soak run is reproducible. Agents are laid out tenant-major (all of tenant 0,
// then tenant 1, …); each hostname carries its tenant and agent index so cohorts
// are distinguishable in server logs and audit events. Server-side the org is
// assigned from the enrolled device, so a live multi-org run seeds one
// enrollment identity per tenant; the harness models the fan-out and load.
func buildTenantAgents(tenants, perTenant int) []tenantAgent {
	agents := make([]tenantAgent, 0, tenants*perTenant)
	for org := 0; org < tenants; org++ {
		for a := 0; a < perTenant; a++ {
			agents = append(agents, tenantAgent{
				orgIndex:   org,
				agentIndex: a,
				hostname:   fmt.Sprintf("soak-t%d-a%d", org, a),
			})
		}
	}
	return agents
}

// buildHealthSummary builds the node + per-family anomaly-rate summary an agent
// emits by default. The values are deterministic placeholders; the soak proves
// the ingest path and cardinality, not the anomaly math.
func buildHealthSummary(ts int64) *protocol.ControlMessage {
	families := make([]protocol.FamilyAnomalyRate, len(defaultFamilies))
	for i, f := range defaultFamilies {
		families[i] = protocol.FamilyAnomalyRate{Family: f, Rate: 0.01 * float64(i+1)}
	}
	return &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              ts,
		NodeAnomalyRate: 0.02,
		PerFamilyRates:  families,
		SamplerVersion:  "soak",
		ModelVersion:    "soak",
	}
}

// buildDefaultMetricWindow builds a host metric window over the default sampler
// dimensions (not log-rate dims), driving the WS-4 avg-series ingest path.
func buildDefaultMetricWindow(ts int64) *protocol.ControlMessage {
	dims := make([]protocol.MetricDim, len(defaultMetricDimNames))
	for i, name := range defaultMetricDimNames {
		dims[i] = protocol.MetricDim{Name: name, Avg: float64(10 + i)}
	}
	return &protocol.ControlMessage{Type: protocol.MsgAgentMetricWindow, TS: ts, Dims: dims}
}

// buildProcessReport builds a minimal rank-ordered top-N process report — the
// sanitized process snapshot the RLS process table + numeric series ingest.
func buildProcessReport(ts int64) *protocol.ControlMessage {
	const topN = 3
	entries := make([]protocol.ProcessReportEntry, topN)
	for i := range entries {
		entries[i] = protocol.ProcessReportEntry{
			Rank:     safeUint32(i + 1),
			Basename: fmt.Sprintf("proc%d", i+1),
			PID:      safeUint32(1000 + i),
			CPU:      float64(5 - i),
			Mem:      float64(8 - i),
		}
	}
	return &protocol.ControlMessage{Type: protocol.MsgProcessReport, TS: ts, TopN: entries}
}

// defaultTelemetryFrames returns the full default telemetry shape one agent
// emits each cycle, in emission order: health summary, host metric window, and
// process report. No frame asserts an org — the server assigns it.
func defaultTelemetryFrames(ts int64) []*protocol.ControlMessage {
	return []*protocol.ControlMessage{
		buildHealthSummary(ts),
		buildDefaultMetricWindow(ts),
		buildProcessReport(ts),
	}
}

// emitDefaultTelemetry emits the default telemetry shape for opts.telemetryCycles
// cycles (at least one when enabled), driving the WS-4 ingest path under load.
func emitDefaultTelemetry(codec *protocol.Codec, w io.Writer, opts loadOptions) error {
	if !opts.defaultTelemetry {
		return nil
	}
	cycles := opts.telemetryCycles
	if cycles < 1 {
		cycles = 1
	}
	for c := 0; c < cycles; c++ {
		for _, frame := range defaultTelemetryFrames(time.Now().Unix()) {
			payload, err := codec.EncodeControl(frame)
			if err != nil {
				return fmt.Errorf("encode %s: %w", frame.Type, err)
			}
			if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
				return fmt.Errorf("write %s: %w", frame.Type, err)
			}
		}
	}
	return nil
}
