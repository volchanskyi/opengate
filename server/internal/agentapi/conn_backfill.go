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
	// is continuous with it; the store keeps every copy of a replayed batch.
	backfillMetric = "opengate_edge_metric_avg"
	// backfillDimLabel names the dimension the pre-rolled value belongs to.
	backfillDimLabel = "dim"
	// backfillRetentionSecs bounds how old a backfilled sample may be: the store's 30-day keep-time.
	backfillRetentionSecs = 30 * 24 * 3600
	// backfillFutureSkewSecs rejects timestamps further ahead than this many seconds.
	backfillFutureSkewSecs = 3600
	// backfillPersistTimeout bounds a batch's synchronous VM write so a slow backend cannot stall
	// the control loop.
	backfillPersistTimeout = 5 * time.Second
)

// The answers a catch-up batch gets, as opengate_edge_backfill_answers_total names them.
const (
	backfillStored              = "stored"
	backfillDeferredNoGrant     = "deferred_no_grant"
	backfillDeferredBusy        = "deferred_busy"
	backfillDeferredWriteFailed = "deferred_write_failed"
	backfillDeferredRefused     = "deferred_refused"
)

// handleMetricBackfillBatch writes a batch at its own timestamps under the connection's tenant and
// answers it once: an ack when stored or filtered, a deferral otherwise.
func (a *AgentConn) handleMetricBackfillBatch(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		a.logger.Debug("ignoring backfill batch: capability not advertised", "device_id", a.DeviceID)
		return nil
	}
	if a.telemetry == nil {
		return a.deferBackfill(backfillDeferredRefused, minRetryAfter)
	}
	if payloadLen > maxTelemetryPayloadBytes {
		a.dropTelemetry("payload_too_large", "type", protocol.MsgMetricBackfillBatch, "bytes", payloadLen)
		return a.deferBackfill(backfillDeferredRefused, minRetryAfter)
	}
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		a.dropTelemetry("tenant_missing", "type", protocol.MsgMetricBackfillBatch)
		return a.deferBackfill(backfillDeferredRefused, minRetryAfter)
	}

	leave, admission := a.scheduler.Admit(a.DeviceID, tenant.TenantID)
	if !admission.Admitted {
		answer := backfillDeferredBusy
		if admission.Reason == BackfillNoGrant {
			answer = backfillDeferredNoGrant
		}
		return a.deferBackfill(answer, time.Duration(admission.RetryAfter)*time.Second)
	}
	stored := a.persistBackfill(ctx, tenant, msg)
	leave()
	if !stored {
		return a.deferBackfill(backfillDeferredWriteFailed, minRetryAfter)
	}
	a.observeBackfillAnswer(backfillStored)
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgMetricBackfillAck,
		Tier:   msg.Tier,
		Cursor: msg.Cursor,
	})
}

// persistBackfill writes the batch's samples inside retention and the vitals vocabulary, and
// reports whether the batch counts as stored; a batch whose every sample was filtered is stored.
func (a *AgentConn) persistBackfill(ctx context.Context, tenant dbtx.Tenant, msg *protocol.ControlMessage) bool {
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
	samples = a.keepWritable(protocol.MsgMetricBackfillBatch, samples)
	if len(samples) == 0 {
		return true
	}
	jobCtx, cancel := context.WithTimeout(ctx, backfillPersistTimeout)
	defer cancel()
	if err := a.telemetry.WriteSamples(jobCtx, tenant.TenantID, a.DeviceID, samples); err != nil {
		a.logger.Warn("backfill persist failed; deferring the batch", "device_id", a.DeviceID, "error", err)
		return false
	}
	return true
}

// deferBackfill tells the machine to keep the batch and send it again after retry.
func (a *AgentConn) deferBackfill(answer string, retry time.Duration) error {
	a.observeBackfillAnswer(answer)
	return a.sendControl(&protocol.ControlMessage{
		Type:       protocol.MsgDeferBackfill,
		RetryAfter: clampNonNegativeUint32(int(max(retry, minRetryAfter) / time.Second)),
	})
}

// observeBackfillAnswer counts one answer beside the places still held.
func (a *AgentConn) observeBackfillAnswer(answer string) {
	if a.metrics != nil {
		a.metrics.ObserveBackfillAnswer(answer, a.scheduler.ActiveCount())
	}
}

// handleRequestBackfillSlot grants catch-up permission scoped to the connection's tenant; without
// a scheduler or the Backfill capability it does nothing.
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
		a.metrics.ObserveBackfillGrant(decision.Rate)
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:     protocol.MsgGrantBackfill,
		Rate:     decision.Rate,
		Deadline: decision.Deadline,
	})
}
