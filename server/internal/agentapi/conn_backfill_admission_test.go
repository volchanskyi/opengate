package agentapi

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestHandleMetricBackfillBatch_ABatchWithoutAGrantStoresNothing(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	clock, _ := fixedClock()
	ac, buf := backfillConn(t, NewBackfillScheduler(schedCfg(), clock, nil), tenant, true)
	ac.telemetry = writer
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))

	assert.Empty(t, writer.calls)
	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgDeferBackfill, reply.Type)
	assert.Positive(t, reply.RetryAfter)
	assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_no_grant")), 0)
}

func TestHandleMetricBackfillBatch_ABatchPastItsTenantsPlacesIsToldToWait(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m
	// The places are booked against the connection's tenant, never one the message names.
	for range DefaultBackfillSchedulerConfig().PerTenantMax {
		admitted(t, ac.scheduler, tenant)
	}

	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))

	assert.Empty(t, writer.calls)
	assert.Equal(t, protocol.MsgDeferBackfill, readReply(t, ac, buf).Type)
	assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_busy")), 0)
}

func TestHandleMetricBackfillBatch_AStoredBatchGivesItsPlaceBack(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), recentBatch(), 128))

	receiveTelemetryCall(t, writer.calls)
	assert.Equal(t, protocol.MsgMetricBackfillAck, readReply(t, ac, buf).Type)
	assert.Zero(t, buf.Len(), "exactly one answer")
	assert.Equal(t, 0, ac.scheduler.ActiveCount())
	assert.InDelta(t, 1, promtestutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("stored")), 0)
}

func TestHandleMetricBackfillBatch_BatchesBeingWrittenHoldAtMostTheGlobalPlaces(t *testing.T) {
	cfg := DefaultBackfillSchedulerConfig()
	s := NewBackfillScheduler(cfg, nil, nil)
	writer := &recordingTelemetryWriter{
		calls: make(chan telemetryWriteCall, cfg.MaxConcurrent+1),
		block: make(chan struct{}),
	}

	type batchConn struct {
		ac  *AgentConn
		buf *bytes.Buffer
		ctx context.Context
	}
	conns := make([]batchConn, cfg.MaxConcurrent+1)
	for i := range conns {
		tenant := uuid.New()
		ac, buf := backfillConn(t, s, tenant, true)
		ac.telemetry = writer
		s.RequestSlot(ac.DeviceID, tenant, SlotRequest{})
		conns[i] = batchConn{ac: ac, buf: buf, ctx: tenantCtx(tenant)}
	}

	done := make(chan struct{}, cfg.MaxConcurrent)
	for _, c := range conns[:cfg.MaxConcurrent] {
		go func() {
			assert.NoError(t, c.ac.handleMetricBackfillBatch(c.ctx, recentBatch(), 128))
			done <- struct{}{}
		}()
	}
	require.Eventually(t, func() bool { return writer.count.Load() == int64(cfg.MaxConcurrent) },
		5*time.Second, 10*time.Millisecond)
	assert.Equal(t, cfg.MaxConcurrent, s.ActiveCount())

	last := conns[cfg.MaxConcurrent]
	require.NoError(t, last.ac.handleMetricBackfillBatch(last.ctx, recentBatch(), 128))
	assert.Equal(t, protocol.MsgDeferBackfill, readReply(t, last.ac, last.buf).Type)

	close(writer.block)
	for range cfg.MaxConcurrent {
		<-done
	}
	assert.Equal(t, 0, s.ActiveCount())
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
