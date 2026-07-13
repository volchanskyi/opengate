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
)

func (a *AgentConn) handleAgentHealthSummary(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgAgentHealthSummary, msg.TS, payloadLen) {
		return nil
	}
	ts := telemetryTimestamp(msg.TS)
	var samples []telemetry.Sample
	// A WS-19 breach-only summary carries no sampler computation; writing a zero
	// anomaly-rate sample for it would be misleading, so the anomaly series is
	// recorded only when the summary actually holds a sampler result.
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
		for _, family := range msg.PerFamilyRates {
			samples = append(samples, telemetry.Sample{
				Name:   "opengate_edge_family_anomaly_rate",
				Value:  family.Rate,
				TS:     ts,
				Labels: map[string]string{"family": family.Family},
			})
		}
	}
	samples = append(samples, alertBreachSamples(msg.Breaches, ts)...)
	if len(samples) == 0 {
		return nil
	}
	a.persistTelemetry(ctx, func(jobCtx context.Context, tenant dbtx.Tenant) error {
		return a.telemetry.WriteSamples(jobCtx, tenant.OrgID, a.DeviceID, samples)
	})
	return nil
}

func (a *AgentConn) handleAgentMetricWindow(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgAgentMetricWindow, msg.TS, payloadLen) {
		return nil
	}
	ts := telemetryTimestamp(msg.TS)
	samples := make([]telemetry.Sample, 0, len(msg.Dims))
	for _, dim := range msg.Dims {
		samples = append(samples, telemetry.Sample{
			Name:   "opengate_edge_metric_avg",
			Value:  dim.Avg,
			TS:     ts,
			Labels: map[string]string{"dim": dim.Name},
		})
	}
	a.persistTelemetry(ctx, func(jobCtx context.Context, tenant dbtx.Tenant) error {
		return a.telemetry.WriteSamples(jobCtx, tenant.OrgID, a.DeviceID, samples)
	})
	return nil
}

func (a *AgentConn) handleProcessReport(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if (a.telemetry == nil && a.processes == nil) || !a.acceptTelemetry(protocol.MsgProcessReport, msg.TS, payloadLen) {
		return nil
	}
	ts := telemetryTimestamp(msg.TS)
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
	a.persistTelemetry(ctx, func(jobCtx context.Context, tenant dbtx.Tenant) error {
		if a.processes != nil {
			if err := a.processes.UpsertReport(jobCtx, a.DeviceID, ts, processSamples); err != nil {
				return err
			}
		}
		if a.telemetry != nil {
			return a.telemetry.WriteSamples(jobCtx, tenant.OrgID, a.DeviceID, numericSamples)
		}
		return nil
	})
	return nil
}

func (a *AgentConn) handleHealthWindowResponse(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil || !a.acceptTelemetry(protocol.MsgHealthWindowResponse, msg.TS, payloadLen) {
		return nil
	}
	var samples []telemetry.Sample
	for _, summary := range msg.Summaries {
		ts := telemetryTimestamp(summary.TS)
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
			samples = append(samples, telemetry.Sample{
				Name:   "opengate_edge_family_anomaly_rate",
				Value:  family.Rate,
				TS:     ts,
				Labels: map[string]string{"family": family.Family, "source": "health_window"},
			})
		}
	}
	a.persistTelemetry(ctx, func(jobCtx context.Context, tenant dbtx.Tenant) error {
		return a.telemetry.WriteSamples(jobCtx, tenant.OrgID, a.DeviceID, samples)
	})
	return nil
}

func (a *AgentConn) acceptTelemetry(msgType protocol.ControlMessageType, ts int64, payloadLen int) bool {
	if payloadLen > maxTelemetryPayloadBytes {
		a.dropTelemetry("payload_too_large", "type", msgType, "bytes", payloadLen)
		return false
	}
	if ts <= 0 {
		return a.acceptedTelemetry(msgType)
	}
	if a.telemetryLast == nil {
		a.telemetryLast = make(map[protocol.ControlMessageType]int64)
	}
	if last, ok := a.telemetryLast[msgType]; ok && ts-last < minTelemetryIntervalSeconds {
		a.dropTelemetry("interval_floor", "type", msgType, "ts", ts, "last_ts", last)
		return false
	}
	a.telemetryLast[msgType] = ts
	return a.acceptedTelemetry(msgType)
}

// acceptedTelemetry records one accepted telemetry message against the ingest
// counter and returns true, so callers can `return a.acceptedTelemetry(...)`.
func (a *AgentConn) acceptedTelemetry(msgType protocol.ControlMessageType) bool {
	if a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryIngest(string(msgType))
	}
	return true
}

func (a *AgentConn) persistTelemetry(ctx context.Context, fn func(context.Context, dbtx.Tenant) error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		a.dropTelemetry("tenant_missing")
		return
	}
	if a.telemetrySlots == nil {
		a.telemetrySlots = make(chan struct{}, telemetryConcurrentWrites)
	}
	select {
	case a.telemetrySlots <- struct{}{}:
		go func() {
			defer func() { <-a.telemetrySlots }()
			jobCtx, cancel := context.WithTimeout(ctx, telemetryPersistTimeout)
			defer cancel()
			if err := fn(jobCtx, tenant); err != nil {
				a.dropTelemetry("persist_failed", "error", err)
			}
		}()
	default:
		a.dropTelemetry("persist_slots_full")
	}
}

func (a *AgentConn) dropTelemetry(reason string, args ...any) {
	a.telemetryDrops.Add(1)
	if a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryDrop(reason)
	}
	if a.logger != nil {
		a.logger.Debug("dropping edge sentinel telemetry", append([]any{"device_id", a.DeviceID, "reason", reason}, args...)...)
	}
}

func telemetryTimestamp(ts int64) time.Time {
	if ts <= 0 {
		return time.Now().UTC()
	}
	return time.Unix(ts, 0).UTC()
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
