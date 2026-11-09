package agentapi

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

var errBackfillWrite = errors.New("backfill write failed")

func backfillConn(t *testing.T, s *BackfillScheduler, tenant uuid.UUID, capable bool) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:  uuid.New(),
		TenantID:  tenant,
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

// ingestConn builds a connection whose machine already holds a catch-up grant.
func ingestConn(t *testing.T, tenant uuid.UUID, writer telemetry.NumericWriter, capable bool) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:  uuid.New(),
		TenantID:  tenant,
		stream:    &buf,
		codec:     &protocol.Codec{},
		telemetry: writer,
		scheduler: NewBackfillScheduler(DefaultBackfillSchedulerConfig(), nil, nil),
		logger:    testLogger(),
	}
	ac.scheduler.RequestSlot(ac.DeviceID, tenant, SlotRequest{})
	if capable {
		ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}
	}
	return ac, &buf
}

func tenantCtx(tenant uuid.UUID) context.Context {
	return dbtx.WithTenant(context.Background(), tenant, false)
}

func readReply(t *testing.T, ac *AgentConn, buf *bytes.Buffer) *protocol.ControlMessage {
	t.Helper()
	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

func TestHandleRequestBackfillSlot_GrantsAndBooksNoPlace(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	tenant := uuid.New()
	ac, buf := backfillConn(t, s, tenant, true)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type:           protocol.MsgRequestBackfillSlot,
		PendingSamples: 5000,
		OldestTS:       1_699_000_000,
	}))

	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgGrantBackfill, reply.Type)
	assert.GreaterOrEqual(t, reply.Rate, schedCfg().MinGrantRate)
	assert.Equal(t, clock().Add(schedCfg().GrantTTL).Unix(), reply.Deadline)
	assert.Equal(t, 0, s.ActiveCount(), "permission to start holds no place")
}

func TestHandleRequestBackfillSlot_GrantsWhileEveryPlaceIsTaken(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	for range schedCfg().MaxConcurrent {
		admitted(t, s, uuid.New())
	}
	ac, buf := backfillConn(t, s, uuid.New(), true)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot}))

	assert.Equal(t, protocol.MsgGrantBackfill, readReply(t, ac, buf).Type)
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

func TestHandleMetricBackfillBatch_WritesHistoricalSamplesAndAcks(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, true)

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

	// The context carries the connection's tenant, which scopes the write.
	ctx := dbtx.WithTenant(context.Background(), tenant, false)
	require.NoError(t, ac.handleMetricBackfillBatch(ctx, msg, 256))

	call := <-writer.calls
	assert.Equal(t, tenant, call.tenantID, "write is scoped to the connection tenant")
	assert.Equal(t, ac.DeviceID, call.deviceID)
	require.Len(t, call.samples, 2, "the out-of-retention sample is clamped away")
	for _, s := range call.samples {
		assert.Equal(t, backfillMetric, s.Name, "backfill lands in the raw avg series the charts read")
		assert.Contains(t, s.Labels, backfillDimLabel)
	}
	assert.Equal(t, "cpu.total", call.samples[0].Labels[backfillDimLabel])
	assert.Equal(t, (now - 7200), call.samples[0].TS.Unix(), "original historical timestamp is preserved")

	ack := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgMetricBackfillAck, ack.Type)
	assert.Equal(t, protocol.BackfillTierRollup1m, ack.Tier)
	assert.Equal(t, now-7140, ack.Cursor)
}

func TestHandleMetricBackfillBatch_AllClampedStillAcksToUnstick(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, true)
	now := time.Now().Unix()
	msg := &protocol.ControlMessage{
		Type: protocol.MsgMetricBackfillBatch,
		Tier: protocol.BackfillTierRollup1h,
		BackfillSamples: []protocol.BackfillSample{
			{Name: "cpu.total", TS: now - backfillRetentionSecs - 100, Value: 1.0},
		},
		Cursor: now - backfillRetentionSecs - 100,
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), msg, 128))

	assert.Empty(t, writer.calls)
	ack := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgMetricBackfillAck, ack.Type)
	assert.Equal(t, msg.Cursor, ack.Cursor)
}

type erroringTelemetryWriter struct{ calls int }

func (e *erroringTelemetryWriter) WriteSamples(context.Context, uuid.UUID, uuid.UUID, []telemetry.Sample) error {
	e.calls++
	return errBackfillWrite
}

func TestHandleMetricBackfillBatch_AFailedWriteIsToldToWait(t *testing.T) {
	tenant := uuid.New()
	writer := &erroringTelemetryWriter{}
	ac, buf := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))

	assert.Equal(t, 1, writer.calls, "the write was attempted")
	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgDeferBackfill, reply.Type, "the machine keeps the batch and sends it again")
	assert.Positive(t, reply.RetryAfter)
	assert.Zero(t, buf.Len(), "exactly one answer")
	assert.Equal(t, 0, ac.scheduler.ActiveCount(), "the place is given back after a failed write")
	assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_write_failed")), 0)
}

func TestHandleMetricBackfillBatch_RefusedBatchesAreToldToWait(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}

	for _, c := range []struct {
		name       string
		ctx        context.Context
		payloadLen int
		telemetry  telemetry.NumericWriter
	}{
		{"a batch past the payload bound", tenantCtx(tenant), maxTelemetryPayloadBytes + 1, writer},
		{"a connection with no tenant", context.Background(), 128, writer},
		{"a server with no metrics store", tenantCtx(tenant), 128, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			ac, buf := ingestConn(t, tenant, c.telemetry, true)
			m := appmetrics.NewMetrics(prometheus.NewRegistry())
			ac.metrics = m

			require.NoError(t, ac.handleMetricBackfillBatch(c.ctx, recentBatch(), c.payloadLen))

			assert.Empty(t, writer.calls, "nothing is written")
			assert.Equal(t, protocol.MsgDeferBackfill, readReply(t, ac, buf).Type)
			assert.Zero(t, buf.Len(), "exactly one answer")
			assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_refused")), 0)
		})
	}
}

func TestHandleMetricBackfillBatch_IgnoredWithoutCapability(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, false)
	msg := &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRecent60s,
		BackfillSamples: []protocol.BackfillSample{{Name: "cpu.total", TS: time.Now().Unix() - 100, Value: 1}},
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), msg, 128))
	assert.Empty(t, writer.calls, "no write for an agent without the Backfill capability")
	assert.Zero(t, buf.Len(), "no ack either")
}
