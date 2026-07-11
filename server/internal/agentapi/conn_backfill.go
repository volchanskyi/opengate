package agentapi

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const (
	// backfillMetric is the central series reconnect backfill writes into — the
	// same raw `avg` series live telemetry uses and the WS-6 charts read via
	// *_over_time, so backfilled history is continuous with live telemetry and
	// immediately visible. The agent's Raw10s/1m/1h tiering is a bandwidth
	// optimization (coarser points for older periods); on the server they
	// reconstitute this one series at their native resolution, deduped by VM on
	// (series, timestamp) so replaying a batch is idempotent. The import API
	// preserves each sample's original timestamp, so history lands in its true
	// time bucket rather than at ingest time.
	backfillMetric = "opengate_edge_metric_avg"
	// backfillDimLabel names the dimension the pre-rolled value belongs to.
	backfillDimLabel = "dim"
	// backfillRetentionSecs bounds how old a backfilled sample may be (~90 d);
	// older history stays reachable only via an on-demand deep-history pull.
	backfillRetentionSecs = 90 * 24 * 3600
	// backfillFutureSkewSecs rejects wild-future timestamps (defense in depth;
	// the agent bounds them too).
	backfillFutureSkewSecs = 3600
	// backfillPersistTimeout bounds a single batch's synchronous VM write so a
	// slow backend cannot stall the control loop indefinitely.
	backfillPersistTimeout = 5 * time.Second
)

// handleMetricBackfillBatch persists a tier's pre-rolled historical samples to
// VictoriaMetrics at their original timestamps, then acks so the agent advances
// its durable per-tier watermark. The write is synchronous and the ack is sent
// only on success: a failed write leaves the batch un-acked, so the agent keeps
// its durable data and re-sends from the un-advanced cursor on its next grant
// (idempotent — VM dedups by timestamp). Samples are clamped to retention and
// bounded against wild clocks, and the org is taken from the authenticated
// connection, never the agent's message.
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
	for _, s := range msg.BackfillSamples {
		if s.TS < floor || s.TS > ceil {
			continue // retention clamp + wild-clock guard (defense in depth)
		}
		samples = append(samples, telemetry.Sample{
			Name:   backfillMetric,
			Value:  s.Value,
			TS:     time.Unix(s.TS, 0).UTC(),
			Labels: map[string]string{backfillDimLabel: s.Name},
		})
	}

	if len(samples) > 0 {
		jobCtx, cancel := context.WithTimeout(ctx, backfillPersistTimeout)
		defer cancel()
		if err := a.telemetry.WriteSamples(jobCtx, tenant.OrgID, a.DeviceID, samples); err != nil {
			a.logger.Warn("backfill persist failed; not acking (agent will retry)",
				"device_id", a.DeviceID, "error", err)
			return nil
		}
	}

	// Ack even an all-clamped batch so the agent advances past out-of-retention
	// (or evicted) ranges without stalling.
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgMetricBackfillAck,
		Tier:   msg.Tier,
		Cursor: msg.Cursor,
	})
}

// handleRequestBackfillSlot admits or defers a reconnect-backfill drain through
// the server-coordinated scheduler and replies to the agent with the decision
// (GrantBackfill with a rate + deadline, or DeferBackfill with a retry-after).
//
// The org is taken from the authenticated connection, never from the agent's
// message, so backfill admission is always scoped to the right tenant. A
// connection without a scheduler (test/programmatic) or an agent that never
// advertised the Backfill capability is a silent no-op — the agent falls back
// to holding its durable data and retrying.
func (a *AgentConn) handleRequestBackfillSlot(msg *protocol.ControlMessage) error {
	if a.scheduler == nil {
		return nil
	}
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		a.logger.Debug("ignoring backfill slot request: capability not advertised", "device_id", a.DeviceID)
		return nil
	}

	decision := a.scheduler.RequestSlot(a.DeviceID, a.OrgID, SlotRequest{
		PendingSamples: msg.PendingSamples,
		OldestTS:       msg.OldestTS,
	})
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
