package agentapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

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
)

// The answers a catch-up batch gets, as opengate_edge_backfill_answers_total names them.
const (
	backfillStored              = "stored"
	backfillDeferredNoGrant     = "deferred_no_grant"
	backfillDeferredBusy        = "deferred_busy"
	backfillDeferredWriteFailed = "deferred_write_failed"
	backfillDeferredRefused     = "deferred_refused"
	backfillDeferredWaited      = "deferred_waited_too_long"
)

// handleMetricBackfillBatch admits a granted batch by the catch-up queue's fill and hands it to
// the writer; the batch is answered once, an ack when stored or filtered, a deferral otherwise.
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

	samples := a.backfillSamples(msg)
	scope := telemetry.Scope{Tenant: tenant.TenantID, Device: a.DeviceID}
	encoded := telemetry.Encode(scope, samples)
	admission := a.scheduler.Admit(CatchUpBatch{
		Device:   a.DeviceID,
		Customer: a.customer(),
		Key:      fmt.Sprintf("%v:%d", msg.Tier, msg.Cursor),
		Size:     encoded.Size(),
		Answer:   a.backfillAnswer(msg.Tier, msg.Cursor, len(samples)),
	})
	switch admission.Verdict {
	case Joined:
		return nil
	case Refused:
		answer := backfillDeferredBusy
		if admission.Reason == BackfillNoGrant {
			answer = backfillDeferredNoGrant
		}
		return a.deferBackfill(answer, time.Duration(admission.RetryAfter)*time.Second)
	}
	a.telemetry.Submit(telemetry.Submission{
		Class:   telemetry.ClassCatchUp,
		Scope:   scope,
		Samples: samples,
		Encoded: encoded,
		Done:    func(outcome telemetry.Outcome) { a.scheduler.Complete(a.DeviceID, outcome) },
	})
	return nil
}

// backfillAnswer answers one batch on this connection once the writer settles it. A send to a
// connection that has gone is dropped: the machine sends the batch again after reconnect.
func (a *AgentConn) backfillAnswer(tier protocol.BackfillTier, cursor int64, readings int) func(telemetry.Outcome) {
	return func(outcome telemetry.Outcome) {
		a.countRefused(protocol.MsgMetricBackfillBatch, outcome, readings)
		var err error
		if outcome.Err != nil {
			a.dropUnstored(protocol.MsgMetricBackfillBatch, outcome.Err)
			err = a.deferUnstored(outcome.Err)
		} else {
			a.observeBackfillAnswer(backfillStored)
			err = a.sendControl(&protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck, Tier: tier, Cursor: cursor})
		}
		if err != nil {
			a.logger.Debug("backfill answer not sent", "device_id", a.DeviceID, "error", err)
		}
	}
}

// deferUnstored tells the machine to send a batch the writer did not store again, counted
// under the writer's reason.
func (a *AgentConn) deferUnstored(err error) error {
	switch {
	case errors.Is(err, telemetry.ErrQueueFull):
		return a.deferBackfill(backfillDeferredBusy, minRetryAfter)
	case errors.Is(err, telemetry.ErrWaitedTooLong):
		return a.deferBackfill(backfillDeferredWaited, minRetryAfter)
	default:
		return a.deferBackfill(backfillDeferredWriteFailed, minRetryAfter)
	}
}

// backfillSamples keeps the batch's samples inside retention and the vitals vocabulary, counting
// each kind of drop once per batch.
func (a *AgentConn) backfillSamples(msg *protocol.ControlMessage) []telemetry.Sample {
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
	return samples
}

// customer is the organization this machine's catch-up is shared under; an unresolved one is
// the unassigned group, uuid.Nil.
func (a *AgentConn) customer() uuid.UUID {
	a.metaMu.RLock()
	defer a.metaMu.RUnlock()
	return a.organizationID
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

	decision := a.scheduler.RequestSlot(a.DeviceID, a.customer(), SlotRequest{
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
