package main

import (
	"fmt"
	"io"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// tenantAgent is one deterministic (tenant, agent) slot in an N-tenant × M-agent
// soak plan: a stable tenant index and a tenant-tagged hostname.
type tenantAgent struct {
	tenantIndex int
	agentIndex  int
	hostname    string
}

// buildHealthSummary builds the node and per-family anomaly-rate summary with placeholder values.
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

// buildDefaultMetricWindow builds a host metric window over the default sampler dimensions.
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

// defaultTelemetryFrames returns one cycle of default telemetry in emission order: health
// summary, host metric window, process report. No frame asserts a tenant.
func defaultTelemetryFrames(ts int64) []*protocol.ControlMessage {
	return []*protocol.ControlMessage{
		buildHealthSummary(ts),
		buildDefaultMetricWindow(ts),
		buildProcessReport(ts),
	}
}

// emitDefaultTelemetry emits the default telemetry shape for opts.telemetryCycles cycles,
// at least one when enabled.
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
