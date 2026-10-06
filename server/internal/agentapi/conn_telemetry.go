package agentapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const (
	maxTelemetryPayloadBytes    = 64 * 1024
	minTelemetryIntervalSeconds = 10
	telemetryPersistTimeout     = 2 * time.Second
	telemetryConcurrentWrites   = 4
	// telemetryFlushMaxSamples caps the coalescing buffer so a burst flushes mid-stream.
	telemetryFlushMaxSamples = 512
)

func (a *AgentConn) handleAgentHealthSummary(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgAgentHealthSummary, msg.TS, payloadLen) {
		return nil
	}
	// The clock correction is counted after the breach vocabulary filter, so a discarded
	// message is reported once, as a drop.
	ts, clamped := clampTelemetryTimestamp(msg.TS, time.Now().UTC())
	var samples []telemetry.Sample
	// A breach-only summary carries no sampler computation, so the anomaly series is
	// recorded only when the summary holds a sampler result.
	if msg.SamplerVersion != "" || len(msg.PerFamilyRates) > 0 {
		samples = append(samples, telemetry.Sample{
			Name:  "opengate_edge_node_anomaly_rate",
			Value: msg.NodeAnomalyRate,
			TS:    ts,
			Labels: map[string]string{
				"sampler_ver": msg.SamplerVersion,
				"model_ver":   msg.ModelVersion,
			},
		})
		unknownFamilies := 0
		for _, family := range msg.PerFamilyRates {
			if !isAnomalyFamily(family.Family) {
				unknownFamilies++
				continue
			}
			samples = append(samples, telemetry.Sample{
				Name:   "opengate_edge_family_anomaly_rate",
				Value:  family.Rate,
				TS:     ts,
				Labels: map[string]string{"family": family.Family},
			})
		}
		// The counter moves once per message however many families were unlisted; the allowlist
		// keeps the family label, and central cardinality, out of agent control.
		if unknownFamilies > 0 {
			a.dropTelemetry("unknown_family", "type", protocol.MsgAgentHealthSummary,
				"unknown", unknownFamilies, "families", len(msg.PerFamilyRates))
		}
	}
	samples = append(samples, alertBreachSamples(msg.Breaches, ts)...)
	// Coverage is state the message produced even with no sample, so a summary that
	// recorded coverage counts as kept.
	recordedCoverage := a.recordRuleCoverage(ctx, msg.RuleCoverage)
	if len(samples) == 0 {
		if !recordedCoverage {
			a.dropTelemetry("empty_summary", "type", protocol.MsgAgentHealthSummary)
		}
		return nil
	}
	a.observeClockClamp(clamped)
	a.bufferTelemetry(ctx, samples)
	return nil
}

func (a *AgentConn) handleAgentMetricWindow(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgAgentMetricWindow, msg.TS, payloadLen) {
		return nil
	}
	if len(msg.Dims) == 0 {
		a.dropTelemetry("empty_dims", "type", protocol.MsgAgentMetricWindow)
		return nil
	}
	ts := a.telemetryTimestamp(msg.TS)
	samples := make([]telemetry.Sample, 0, len(msg.Dims))
	unknown := 0
	for _, dim := range msg.Dims {
		if !isVitalDim(dim.Name) {
			unknown++
			continue
		}
		samples = append(samples, telemetry.Sample{
			Name:   "opengate_edge_metric_avg",
			Value:  dim.Avg,
			TS:     ts,
			Labels: map[string]string{"dim": dim.Name},
		})
	}
	// The counter moves once per window however many dims were unlisted; the allowlist
	// keeps the dim label, and central cardinality, out of agent control.
	if unknown > 0 {
		a.dropTelemetry("unknown_dim", "type", protocol.MsgAgentMetricWindow,
			"unknown", unknown, "dims", len(msg.Dims))
	}
	if len(samples) == 0 {
		return nil
	}
	a.bufferTelemetry(ctx, samples)
	return nil
}

func (a *AgentConn) handleProcessReport(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if (a.telemetry == nil && a.processes == nil) || !a.acceptTelemetry(protocol.MsgProcessReport, msg.TS, payloadLen) {
		return nil
	}
	if len(msg.TopN) == 0 {
		a.dropTelemetry("empty_processes", "type", protocol.MsgProcessReport)
		return nil
	}
	ts := a.telemetryTimestamp(msg.TS)
	processSamples := make([]telemetry.ProcessSample, 0, len(msg.TopN))
	numericSamples := make([]telemetry.Sample, 0, len(msg.TopN)*2)
	for _, entry := range msg.TopN {
		processSamples = append(processSamples, telemetry.ProcessSample{
			Rank:        entry.Rank,
			Basename:    sanitizeProcessBasename(entry.Basename),
			CmdlineHash: entry.CmdlineHash,
			PID:         entry.PID,
			CPU:         entry.CPU,
			Mem:         entry.Mem,
		})
		rank := fmt.Sprintf("%d", entry.Rank)
		numericSamples = append(numericSamples,
			telemetry.Sample{
				Name:   "opengate_edge_process_cpu_percent",
				Value:  entry.CPU,
				TS:     ts,
				Labels: map[string]string{"rank": rank},
			},
			telemetry.Sample{
				Name:   "opengate_edge_process_mem_percent",
				Value:  entry.Mem,
				TS:     ts,
				Labels: map[string]string{"rank": rank},
			},
		)
	}
	// Report rows have their own persist slot; exactly one of the two writes owns the
	// report's accounting, so a failed flush does not double-count it.
	if a.processes != nil {
		owned := 0
		if a.telemetry == nil {
			owned = 1
		}
		a.persistTelemetry(ctx, owned, func(jobCtx context.Context, _ dbtx.Tenant) error {
			return a.processes.UpsertReport(jobCtx, a.DeviceID, ts, processSamples)
		})
	}
	if a.telemetry != nil {
		a.bufferTelemetry(ctx, numericSamples)
	}
	return nil
}

func (a *AgentConn) handleHealthWindowResponse(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgHealthWindowResponse, msg.TS, payloadLen) {
		return nil
	}
	if len(msg.Summaries) == 0 {
		a.dropTelemetry("empty_summaries", "type", protocol.MsgHealthWindowResponse)
		return nil
	}
	var samples []telemetry.Sample
	unknownFamilies := 0
	for _, summary := range msg.Summaries {
		ts := a.telemetryTimestamp(summary.TS)
		samples = append(samples, telemetry.Sample{
			Name:  "opengate_edge_node_anomaly_rate",
			Value: summary.NodeAnomalyRate,
			TS:    ts,
			Labels: map[string]string{
				"sampler_ver": summary.SamplerVersion,
				"model_ver":   summary.ModelVersion,
				"source":      "health_window",
			},
		})
		for _, family := range summary.PerFamilyRates {
			if !isAnomalyFamily(family.Family) {
				unknownFamilies++
				continue
			}
			samples = append(samples, telemetry.Sample{
				Name:   "opengate_edge_family_anomaly_rate",
				Value:  family.Rate,
				TS:     ts,
				Labels: map[string]string{"family": family.Family, "source": "health_window"},
			})
		}
	}
	// One response is one message however many summaries it replayed, so the counter moves once.
	if unknownFamilies > 0 {
		a.dropTelemetry("unknown_family", "type", protocol.MsgHealthWindowResponse,
			"unknown", unknownFamilies, "summaries", len(msg.Summaries))
	}
	a.bufferTelemetry(ctx, samples)
	return nil
}

// bufferTelemetry appends samples to the coalescing buffer on the read-loop goroutine, so it
// needs no lock; each call books one message against the buffer and flushes at the size cap.
func (a *AgentConn) bufferTelemetry(ctx context.Context, samples []telemetry.Sample) {
	if a.telemetry == nil || len(samples) == 0 {
		return
	}
	a.telemetryBuf = append(a.telemetryBuf, samples...)
	a.telemetryBufMsgs++
	if len(a.telemetryBuf) >= telemetryFlushMaxSamples {
		a.flushTelemetry(ctx)
	}
}

// flushTelemetry drains the coalescing buffer into one WriteSamples through one persist slot,
// so a heartbeat's burst is a single write. A no-op when the buffer is empty.
func (a *AgentConn) flushTelemetry(ctx context.Context) {
	if len(a.telemetryBuf) == 0 {
		return
	}
	batch := a.telemetryBuf
	msgs := a.telemetryBufMsgs
	a.telemetryBuf = nil
	a.telemetryBufMsgs = 0
	a.persistTelemetry(ctx, msgs, func(jobCtx context.Context, tenant dbtx.Tenant) error {
		return a.telemetry.WriteSamples(jobCtx, tenant.TenantID, a.DeviceID, batch)
	})
}

func sanitizeProcessBasename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return "[redacted]"
	}
	if idx := strings.LastIndexAny(name, `/\`); idx >= 0 && idx < len(name)-1 {
		name = name[idx+1:]
	}
	return name
}
