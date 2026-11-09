package agentapi

import (
	"context"
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
	// A breach-only summary carries no sampler computation, so the anomaly series is
	// recorded only when the summary holds a sampler result.
	sampled := msg.SamplerVersion != "" || len(msg.PerFamilyRates) > 0
	kind := summaryBreach
	if sampled {
		kind = summaryAnomaly
	}
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgAgentHealthSummary, kind, msg.TS, payloadLen) {
		return nil
	}
	// The clock correction is counted after the breach vocabulary filter, so a discarded
	// message is reported once, as a drop.
	ts, clamped := clampTelemetryTimestamp(msg.TS, time.Now().UTC())
	var samples []telemetry.Sample
	if sampled {
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
	a.bufferTelemetry(ctx, protocol.MsgAgentHealthSummary, samples)
	return nil
}

func (a *AgentConn) handleAgentMetricWindow(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgAgentMetricWindow, floorPerType, msg.TS, payloadLen) {
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
	a.bufferTelemetry(ctx, protocol.MsgAgentMetricWindow, samples)
	return nil
}

func (a *AgentConn) handleHealthWindowResponse(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgHealthWindowResponse, floorPerType, msg.TS, payloadLen) {
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
	a.bufferTelemetry(ctx, protocol.MsgHealthWindowResponse, samples)
	return nil
}

// bufferTelemetry appends the samples the store accepts to the read loop's coalescing buffer, one
// message a call, and flushes at the size cap; the read loop alone touches it, so it has no lock.
func (a *AgentConn) bufferTelemetry(ctx context.Context, msgType protocol.ControlMessageType, samples []telemetry.Sample) {
	if a.telemetry == nil {
		return
	}
	samples = a.keepWritable(msgType, samples)
	if len(samples) == 0 {
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

// keepWritable keeps the samples the store accepts on their own; the rest of a message count as
// one invalid_value drop however many it lost.
func (a *AgentConn) keepWritable(msgType protocol.ControlMessageType, samples []telemetry.Sample) []telemetry.Sample {
	kept := samples[:0]
	refused := 0
	for _, s := range samples {
		if err := telemetry.CheckSample(s); err != nil {
			refused++
			continue
		}
		kept = append(kept, s)
	}
	if refused > 0 {
		a.dropTelemetry("invalid_value", "type", msgType, "refused", refused, "readings", len(samples))
	}
	return kept
}
