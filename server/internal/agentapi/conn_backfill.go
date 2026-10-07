package agentapi

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const (
	// backfillMetric is the raw avg series that live telemetry also writes, so backfilled history
	// is continuous with it and VM dedups a replayed batch by series and timestamp.
	backfillMetric = "opengate_edge_metric_avg"
	// backfillDimLabel names the dimension the pre-rolled value belongs to.
	backfillDimLabel = "dim"
	// backfillRetentionSecs bounds how old a backfilled sample may be (90 days).
	backfillRetentionSecs = 90 * 24 * 3600
	// backfillFutureSkewSecs rejects timestamps further ahead than this many seconds.
	backfillFutureSkewSecs = 3600
	// backfillPersistTimeout bounds a batch's synchronous VM write so a slow backend cannot stall
	// the control loop.
	backfillPersistTimeout = 5 * time.Second
)

// handleMetricBackfillBatch persists pre-rolled samples at their original timestamps and acks;
// a failed write leaves the batch un-acked. The tenant is the connection's, never the message's.
func (a *AgentConn) handleMetricBackfillBatch(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if a.telemetry == nil {
		return nil
	}
	if payloadLen > maxTelemetryPayloadBytes {
		a.dropTelemetry("payload_too_large", "type", protocol.MsgMetricBackfillBatch, "bytes", payloadLen)
		return nil
	}
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		a.logger.Debug("ignoring backfill batch: capability not advertised", "device_id", a.DeviceID)
		return nil
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		a.dropTelemetry("tenant_missing", "type", protocol.MsgMetricBackfillBatch)
		return nil
	}

	now := time.Now().Unix()
	floor := now - backfillRetentionSecs
	ceil := now + backfillFutureSkewSecs
	samples := make([]telemetry.Sample, 0, len(msg.BackfillSamples))
	skipped := 0
	unknown := 0
	for _, s := range msg.BackfillSamples {
		if s.TS < floor || s.TS > ceil {
			skipped++
			continue
		}
		// A dimension outside the live vocabulary would open central series cardinality.
		if !isVitalDim(s.Name) {
			unknown++
			continue
		}
		samples = append(samples, telemetry.Sample{
			Name:   backfillMetric,
			Value:  s.Value,
			TS:     time.Unix(s.TS, 0).UTC(),
			Labels: map[string]string{backfillDimLabel: s.Name},
		})
	}
	// A batch counts once per drop reason however many samples it lost; the sample count rides the log.
	if skipped > 0 {
		a.dropTelemetry("backfill_out_of_retention", "type", protocol.MsgMetricBackfillBatch,
			"tier", msg.Tier, "skipped", skipped, "batch", len(msg.BackfillSamples))
	}
	if unknown > 0 {
		a.dropTelemetry("unknown_dim", "type", protocol.MsgMetricBackfillBatch,
			"tier", msg.Tier, "unknown", unknown, "batch", len(msg.BackfillSamples))
	}

	if len(samples) > 0 {
		jobCtx, cancel := context.WithTimeout(ctx, backfillPersistTimeout)
		defer cancel()
		if err := a.telemetry.WriteSamples(jobCtx, tenant.TenantID, a.DeviceID, samples); err != nil {
			a.logger.Warn("backfill persist failed; not acking (agent will retry)",
				"device_id", a.DeviceID, "error", err)
			return nil
		}
	}

	// An all-skipped batch is still acked so the agent advances past out-of-retention ranges.
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgMetricBackfillAck,
		Tier:   msg.Tier,
		Cursor: msg.Cursor,
	})
}

// handleRequestBackfillSlot asks the scheduler to grant or defer a backfill drain, scoped to the
// connection's tenant; without a scheduler or the Backfill capability it does nothing.
func (a *AgentConn) handleRequestBackfillSlot(msg *protocol.ControlMessage) error {
	if a.scheduler == nil {
		return nil
	}
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		a.logger.Debug("ignoring backfill slot request: capability not advertised", "device_id", a.DeviceID)
		return nil
	}

	decision := a.scheduler.RequestSlot(a.DeviceID, a.TenantID, SlotRequest{
		PendingSamples: msg.PendingSamples,
		OldestTS:       msg.OldestTS,
	})
	if a.metrics != nil {
		a.metrics.ObserveBackfillDecision(decision.Grant, decision.Rate, a.scheduler.ActiveCount())
	}
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
