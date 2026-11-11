package agentapi

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// answeredOnce reads the batch's single answer under the write lock the server holds for a whole
// frame, and requires nothing after it.
func answeredOnce(t *testing.T, ac *AgentConn, buf *bytes.Buffer) *protocol.ControlMessage {
	t.Helper()
	ac.writeMu.Lock()
	defer ac.writeMu.Unlock()
	reply := readReply(t, ac, buf)
	assert.Zero(t, buf.Len(), "a batch gets exactly one answer")
	return reply
}

func TestEveryCatchUpBatchIsAnsweredOnceWhateverBecomesOfIt(t *testing.T) {
	cases := []struct {
		name    string
		fail    error
		batch   func() *protocol.ControlMessage
		want    protocol.ControlMessageType
		counted string
	}{
		{name: "stored", batch: recentBatch, want: protocol.MsgMetricBackfillAck, counted: "stored"},
		{name: "filtered whole", batch: staleBatch, want: protocol.MsgMetricBackfillAck, counted: "stored"},
		{name: "the queue is full", fail: telemetry.ErrQueueFull, batch: recentBatch,
			want: protocol.MsgDeferBackfill, counted: "deferred_busy"},
		{name: "it waited too long", fail: telemetry.ErrWaitedTooLong, batch: recentBatch,
			want: protocol.MsgDeferBackfill, counted: "deferred_waited_too_long"},
		{name: "the write failed", fail: fmt.Errorf("%w: store down", telemetry.ErrStoreWrite), batch: recentBatch,
			want: protocol.MsgDeferBackfill, counted: "deferred_write_failed"},
		{name: "the server is shutting down", fail: telemetry.ErrShuttingDown, batch: recentBatch,
			want: protocol.MsgDeferBackfill, counted: "deferred_write_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := uuid.New()
			writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1), fail: tc.fail}
			ac, buf := ingestConn(t, tenant, writer, true)
			m := appmetrics.NewMetrics(prometheus.NewRegistry())
			ac.metrics = m

			require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), tc.batch(), 128))

			assert.Equal(t, tc.want, answeredOnce(t, ac, buf).Type)
			assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues(tc.counted)), 0)
			assert.Zero(t, ac.scheduler.ActiveCount(), "an answered batch gives its place back")
		})
	}
}

func TestACatchUpBatchWithoutAGrantStoresNothing(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	clock, _ := fixedClock()
	ac, buf := backfillConn(t, NewBackfillScheduler(schedCfg(), clock, nil, nil), tenant, true)
	ac.telemetry = writer
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))

	assert.Zero(t, writer.count.Load(), "nothing is queued for a machine without permission")
	reply := answeredOnce(t, ac, buf)
	assert.Equal(t, protocol.MsgDeferBackfill, reply.Type)
	assert.Positive(t, reply.RetryAfter)
	assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_no_grant")), 0)
}

func TestASecondBatchWhileTheFirstIsQueuedIsToldToWait(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 2), block: make(chan struct{})}
	ac, buf := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))

	next := recentBatch()
	next.Cursor++
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), next, 128))

	assert.Equal(t, protocol.MsgDeferBackfill, readReply(t, ac, buf).Type)
	assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_busy")), 0)
	close(writer.block)
	require.Eventually(t, func() bool { return ac.scheduler.ActiveCount() == 0 }, 2*time.Second, 5*time.Millisecond)
}

func TestTheSameBatchSentAgainOnANewConnectionIsWrittenOnceAndAnsweredThere(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 2), block: make(chan struct{})}
	old, oldBuf := ingestConn(t, tenant, writer, true)
	again, againBuf := backfillConn(t, old.scheduler, tenant, true)
	again.DeviceID = old.DeviceID
	again.telemetry = writer

	require.NoError(t, old.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))
	require.NoError(t, again.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))
	close(writer.block)

	require.Eventually(t, func() bool {
		again.writeMu.Lock()
		defer again.writeMu.Unlock()
		return againBuf.Len() > 0
	}, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, protocol.MsgMetricBackfillAck, answeredOnce(t, again, againBuf).Type,
		"the answer goes to the connection the machine holds now")
	assert.Zero(t, oldBuf.Len(), "the departed connection is not answered")
	assert.Equal(t, int64(1), writer.count.Load(), "the copy makes no second write")
}

func TestABatchAnsweredAfterItsMachineLeftIsDroppedAndGivesItsPlaceBack(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1), block: make(chan struct{})}
	ac, buf := ingestConn(t, tenant, writer, true)
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))
	ac.markReleased()

	close(writer.block)
	require.Eventually(t, func() bool { return ac.scheduler.ActiveCount() == 0 }, 2*time.Second, 5*time.Millisecond)
	assert.Zero(t, buf.Len(), "nothing is written to a connection the server let go")
}

func TestAHeartbeatAndAnAlertBehindAHeldCatchUpBatchAreHandledAtOnce(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1), block: make(chan struct{})}
	defer close(writer.block)
	f := alertConn(t)
	ac := f.conn
	ac.TenantID = tenant
	ac.telemetry = writer
	ac.scheduler = NewBackfillScheduler(DefaultBackfillSchedulerConfig(), nil, nil, nil)
	ac.scheduler.RequestSlot(ac.DeviceID, uuid.Nil, SlotRequest{})
	ac.setMeta("linux", "amd64", "0.1.0", []protocol.AgentCapability{protocol.CapBackfill, protocol.CapAlerts})
	var stream bytes.Buffer
	ac.stream = &stream
	ac.codec = &protocol.Codec{}
	writeControlMsg(t, ac.codec, &stream, recentBatch())
	writeControlMsg(t, ac.codec, &stream, &protocol.ControlMessage{Type: protocol.MsgAgentHeartbeat, Timestamp: time.Now().Unix()})
	writeControlMsg(t, ac.codec, &stream, wellFormed(t))
	ctx := tenantCtx(tenant)

	started := time.Now()
	for range 3 {
		require.NoError(t, ac.handleControl(ctx))
	}
	assert.Less(t, time.Since(started), 100*time.Millisecond,
		"the read loop never waits on the store while a catch-up write is held")
	f.reachedStore(t, 1)
}

// staleBatch is one batch whose only reading is older than the store keeps.
func staleBatch() *protocol.ControlMessage {
	ts := time.Now().Unix() - backfillRetentionSecs - 3600
	return &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRecent60s,
		BackfillSamples: []protocol.BackfillSample{{Name: "cpu.total", TS: ts, Value: 1}},
		Cursor:          ts,
	}
}

// recentBatch is one batch of a single reading an hour old.
func recentBatch() *protocol.ControlMessage {
	ts := time.Now().Unix() - 3600
	return &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRecent60s,
		BackfillSamples: []protocol.BackfillSample{{Name: "cpu.total", TS: ts, Value: 1}},
		Cursor:          ts,
	}
}
