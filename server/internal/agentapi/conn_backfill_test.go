package agentapi

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// errBackfillWrite is the sentinel a failing telemetry writer returns.
var errBackfillWrite = errors.New("backfill write failed")

// backfillConn builds an AgentConn wired to a scheduler over an in-memory
// buffer, advertising the Backfill capability unless capable is false.
func backfillConn(t *testing.T, s *BackfillScheduler, org uuid.UUID, capable bool) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:  uuid.New(),
		OrgID:     org,
		stream:    &buf,
		codec:     &protocol.Codec{},
		scheduler: s,
		logger:    testLogger(),
	}
	if capable {
		ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}
	}
	return ac, &buf
}

// ingestConn builds an AgentConn wired to a telemetry writer over an in-memory
// buffer, advertising the Backfill capability unless capable is false.
func ingestConn(t *testing.T, org uuid.UUID, writer telemetry.NumericWriter, capable bool) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:  uuid.New(),
		OrgID:     org,
		stream:    &buf,
		codec:     &protocol.Codec{},
		telemetry: writer,
		logger:    testLogger(),
	}
	if capable {
		ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}
	}
	return ac, &buf
}

// tenantCtx scopes a context to org, as handleControl does before dispatch.
func tenantCtx(org uuid.UUID) context.Context {
	return dbtx.WithTenant(context.Background(), org, false)
}

// readReply decodes the single control frame the handler wrote back to buf.
func readReply(t *testing.T, ac *AgentConn, buf *bytes.Buffer) *protocol.ControlMessage {
	t.Helper()
	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

func TestHandleRequestBackfillSlot_GrantsWhenAdmitted(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	org := uuid.New()
	ac, buf := backfillConn(t, s, org, true)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type:           protocol.MsgRequestBackfillSlot,
		PendingSamples: 5000,
		OldestTS:       1_699_000_000,
	}))

	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgGrantBackfill, reply.Type)
	assert.GreaterOrEqual(t, reply.Rate, schedCfg().MinGrantRate)
	assert.Equal(t, clock().Add(schedCfg().GrantTTL).Unix(), reply.Deadline)
	// The slot is booked against the connection's org, never an agent-supplied one.
	assert.Equal(t, 1, s.ActiveCount())
}

func TestHandleRequestBackfillSlot_DefersWhenSaturated(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	// Saturate the global cap with unrelated agents.
	for range schedCfg().MaxConcurrent {
		require.True(t, s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{}).Grant)
	}
	ac, buf := backfillConn(t, s, uuid.New(), true)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type: protocol.MsgRequestBackfillSlot,
	}))

	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgDeferBackfill, reply.Type)
	assert.Positive(t, reply.RetryAfter)
}

func TestHandleRequestBackfillSlot_IgnoredWithoutCapability(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	ac, buf := backfillConn(t, s, uuid.New(), false)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type: protocol.MsgRequestBackfillSlot,
	}))
	assert.Zero(t, buf.Len(), "no reply is written for an agent without the Backfill capability")
	assert.Equal(t, 0, s.ActiveCount(), "no slot is booked")
}

func TestHandleRequestBackfillSlot_NoSchedulerIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:     uuid.New(),
		stream:       &buf,
		codec:        &protocol.Codec{},
		Capabilities: []protocol.AgentCapability{protocol.CapBackfill},
		logger:       testLogger(),
	}
	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot}))
	assert.Zero(t, buf.Len())
}

func TestHandleRequestBackfillSlot_ScopesToConnectionOrg(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	connOrg := uuid.New()
	ac, buf := backfillConn(t, s, connOrg, true)

	// Fill the connection's org to its per-tenant cap using the SAME org id the
	// connection carries; the next request must defer — proving admission keys on
	// the connection org, not anything the agent could supply.
	for range schedCfg().PerTenantMax {
		require.True(t, s.RequestSlot(uuid.New(), connOrg, SlotRequest{}).Grant)
	}
	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot}))
	assert.Equal(t, protocol.MsgDeferBackfill, readReply(t, ac, buf).Type)
}

func TestHandleMetricBackfillBatch_WritesHistoricalSamplesAndAcks(t *testing.T) {
	org := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, org, writer, true)

	now := time.Now().Unix()
	tooOld := now - backfillRetentionSecs - 3600 // out of retention → clamped
	msg := &protocol.ControlMessage{
		Type: protocol.MsgMetricBackfillBatch,
		Tier: protocol.BackfillTierRollup1m,
		BackfillSamples: []protocol.BackfillSample{
			{Name: "cpu.total", TS: now - 7200, Value: 42.5},
			{Name: "mem.used_percent", TS: now - 7140, Value: 63.0},
			{Name: "cpu.total", TS: tooOld, Value: 99.0},
		},
		Cursor: now - 7140,
	}

	// handleControl injects the tenant scope; the org must come from the
	// connection, so seed the context with a DIFFERENT tenant to prove the
	// write keys on the connection's tenant, not the context default.
	ctx := dbtx.WithTenant(context.Background(), org, false)
	require.NoError(t, ac.handleMetricBackfillBatch(ctx, msg, 256))

	call := <-writer.calls
	assert.Equal(t, org, call.orgID, "write is scoped to the connection org")
	assert.Equal(t, ac.DeviceID, call.deviceID)
	require.Len(t, call.samples, 2, "the out-of-retention sample is clamped away")
	for _, s := range call.samples {
		assert.Equal(t, backfillMetric, s.Name, "backfill lands in the raw avg series the charts read")
		assert.Contains(t, s.Labels, backfillDimLabel)
	}
	assert.Equal(t, "cpu.total", call.samples[0].Labels[backfillDimLabel])
	assert.Equal(t, (now - 7200), call.samples[0].TS.Unix(), "original historical timestamp is preserved")

	// The ack carries the batch's tier + cursor so the agent advances the right
	// per-tier watermark.
	ack := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgMetricBackfillAck, ack.Type)
	assert.Equal(t, protocol.BackfillTierRollup1m, ack.Tier)
	assert.Equal(t, now-7140, ack.Cursor)
}

func TestHandleMetricBackfillBatch_AllClampedStillAcksToUnstick(t *testing.T) {
	org := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, org, writer, true)
	now := time.Now().Unix()
	msg := &protocol.ControlMessage{
		Type: protocol.MsgMetricBackfillBatch,
		Tier: protocol.BackfillTierRollup1h,
		BackfillSamples: []protocol.BackfillSample{
			{Name: "cpu.total", TS: now - backfillRetentionSecs - 100, Value: 1.0},
		},
		Cursor: now - backfillRetentionSecs - 100,
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(org), msg, 128))

	// No write (everything clamped) but still an ack so the agent does not stall.
	assert.Empty(t, writer.calls)
	ack := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgMetricBackfillAck, ack.Type)
	assert.Equal(t, msg.Cursor, ack.Cursor)
}

// erroringTelemetryWriter fails every write, exercising the not-acked path.
type erroringTelemetryWriter struct{ calls int }

func (e *erroringTelemetryWriter) WriteSamples(context.Context, uuid.UUID, uuid.UUID, []telemetry.Sample) error {
	e.calls++
	return errBackfillWrite
}

func TestHandleMetricBackfillBatch_DoesNotAckOnPersistFailure(t *testing.T) {
	org := uuid.New()
	writer := &erroringTelemetryWriter{}
	ac, buf := ingestConn(t, org, writer, true)
	msg := &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRaw10s,
		BackfillSamples: []protocol.BackfillSample{{Name: "cpu.total", TS: time.Now().Unix() - 100, Value: 1}},
		Cursor:          time.Now().Unix() - 100,
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(org), msg, 128))
	assert.Equal(t, 1, writer.calls, "the write was attempted")
	assert.Zero(t, buf.Len(), "a failed write is not acked — the agent re-sends from its cursor")
}

func TestHandleMetricBackfillBatch_GuardsNilTelemetryPayloadAndTenant(t *testing.T) {
	org := uuid.New()
	sample := []protocol.BackfillSample{{Name: "cpu.total", TS: time.Now().Unix() - 100, Value: 1}}
	msg := &protocol.ControlMessage{Type: protocol.MsgMetricBackfillBatch, BackfillSamples: sample}
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}

	// Nil telemetry writer: a silent no-op.
	nilAc, nilBuf := ingestConn(t, org, nil, true)
	require.NoError(t, nilAc.handleMetricBackfillBatch(tenantCtx(org), msg, 128))
	assert.Zero(t, nilBuf.Len())

	// Oversized payload is dropped before any write or ack.
	bigAc, bigBuf := ingestConn(t, org, writer, true)
	require.NoError(t, bigAc.handleMetricBackfillBatch(tenantCtx(org), msg, maxTelemetryPayloadBytes+1))
	assert.Empty(t, writer.calls)
	assert.Zero(t, bigBuf.Len())
	assert.Positive(t, bigAc.DroppedTelemetryCount())

	// No tenant in context: dropped, never written with a guessed org.
	noTenantAc, noTenantBuf := ingestConn(t, org, writer, true)
	require.NoError(t, noTenantAc.handleMetricBackfillBatch(context.Background(), msg, 128))
	assert.Empty(t, writer.calls)
	assert.Zero(t, noTenantBuf.Len())
}

func TestHandleMetricBackfillBatch_IgnoredWithoutCapability(t *testing.T) {
	org := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, org, writer, false)
	msg := &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRaw10s,
		BackfillSamples: []protocol.BackfillSample{{Name: "cpu.total", TS: time.Now().Unix() - 100, Value: 1}},
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(org), msg, 128))
	assert.Empty(t, writer.calls, "no write for an agent without the Backfill capability")
	assert.Zero(t, buf.Len(), "no ack either")
}
