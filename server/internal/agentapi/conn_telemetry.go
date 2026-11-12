package agentapi

import (
	"context"
	"errors"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const (
	maxTelemetryPayloadBytes    = 64 * 1024
	minTelemetryIntervalSeconds = 10
	// telemetryPersistTimeout and telemetryConcurrentWrites bound the alert writes' persist slots.
	telemetryPersistTimeout   = 2 * time.Second
	telemetryConcurrentWrites = 4
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
	a.handOff(ctx, protocol.MsgAgentHealthSummary, samples, nil)
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
		labels, vital := vitalLabels[dim.Name]
		if !vital {
			unknown++
			continue
		}
		samples = append(samples, telemetry.Sample{
			Name:   backfillMetric,
			Value:  dim.Avg,
			TS:     ts,
			Labels: labels,
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
	a.handOff(ctx, protocol.MsgAgentMetricWindow, samples, a.confirmLiveWindow(msg.TS))
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
	a.handOff(ctx, protocol.MsgHealthWindowResponse, samples, nil)
	return nil
}

// ReadingWriter takes a message's readings without waiting on the store; *telemetry.Writer is the
// one the server runs.
type ReadingWriter interface {
	Submit(sub telemetry.Submission)
	// Fill is the share of a class's bound held, which catch-up admission reads.
	Fill(class telemetry.Class) float64
}

// handOff gives one live message's readings to the writer and returns at once. The answer counts
// what the store did not take, and stored runs once every reading is accepted or refused.
func (a *AgentConn) handOff(ctx context.Context, msgType protocol.ControlMessageType, samples []telemetry.Sample, stored func()) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		a.dropTelemetry("tenant_missing", "type", msgType)
		return
	}
	a.telemetry.Submit(telemetry.Submission{
		Class:   telemetry.ClassLive,
		Scope:   telemetry.Scope{Tenant: tenant.TenantID, Device: a.DeviceID},
		Samples: samples,
		Done: func(outcome telemetry.Outcome) {
			a.countRefused(msgType, outcome, len(samples))
			if outcome.Err != nil {
				a.dropUnstored(msgType, outcome.Err)
				return
			}
			if stored != nil {
				stored()
			}
		},
	})
}

// countRefused counts a message whose readings the store refused on their own as one
// invalid_value drop, however many it lost.
func (a *AgentConn) countRefused(msgType protocol.ControlMessageType, outcome telemetry.Outcome, readings int) {
	if outcome.Refused > 0 {
		a.dropTelemetry("invalid_value", "type", msgType, "refused", outcome.Refused, "readings", readings)
	}
}

// dropUnstored counts a message whose readings the writer did not store, under the writer's
// reason.
func (a *AgentConn) dropUnstored(msgType protocol.ControlMessageType, err error) {
	switch {
	case errors.Is(err, telemetry.ErrQueueFull):
		a.dropTelemetry("queue_full", "type", msgType)
	case errors.Is(err, telemetry.ErrTombstoned):
		a.dropTelemetry("tombstoned", "type", msgType)
	case errors.Is(err, telemetry.ErrShuttingDown):
		a.dropTelemetry("shutting_down", "type", msgType)
	case errors.Is(err, telemetry.ErrWaitedTooLong):
		a.dropTelemetry("waited_too_long", "type", msgType)
	default:
		a.dropTelemetry("persist_failed", "type", msgType, "error", err)
	}
}

// confirmLiveWindow tells a machine that reads confirmations its window stamped ts is stored.
func (a *AgentConn) confirmLiveWindow(ts int64) func() {
	return func() {
		if a.requireCapability(protocol.CapLiveConfirmations) != nil {
			return
		}
		if err := a.sendControl(&protocol.ControlMessage{Type: protocol.MsgLiveWindowStored, TS: ts}); err != nil {
			a.logger.Debug("live confirmation not sent", "device_id", a.DeviceID, "ts", ts, "error", err)
		}
	}
}
