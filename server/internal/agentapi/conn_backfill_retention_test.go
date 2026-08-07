package agentapi

import (
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

// TestBackfillOutOfRetentionIsCounted pins that the backfill path's per-sample
// skip is no longer invisible: it files one typed drop per batch carrying the
// skipped count, keeps its own 90 d floor, and still acks so the agent advances.
func TestBackfillOutOfRetentionIsCounted(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	now := time.Now().Unix()
	msg := &protocol.ControlMessage{
		Type: protocol.MsgMetricBackfillBatch,
		Tier: protocol.BackfillTierRaw10s,
		BackfillSamples: []protocol.BackfillSample{
			// Inside the 90 d floor — a 7 d clamp would have truncated this one.
			{Name: testDim, Value: 1, TS: now - 60*24*3600},
			{Name: testDim, Value: 2, TS: now - backfillRetentionSecs - 1},
			{Name: testDim, Value: 3, TS: now + backfillFutureSkewSecs + 1},
		},
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), msg, 512))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 1, "the 60 d sample is inside the backfill floor")
	assert.Equal(t, time.Unix(now-60*24*3600, 0).UTC(), call.samples[0].TS)
	// One batch is one message on the drop counter however many samples it lost;
	// the per-sample count rides the log line.
	assert.InDelta(t, 1, promtestutil.ToFloat64(
		m.EdgeTelemetryDropsTotal.WithLabelValues("backfill_out_of_retention")), 0)
	assert.Equal(t, protocol.MsgMetricBackfillAck, readReply(t, ac, buf).Type)
}

// TestBackfillInRetentionCountsNoDrop pins the negative case: a batch entirely
// inside the retention window files no drop at all.
func TestBackfillInRetentionCountsNoDrop(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	now := time.Now().Unix()
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRaw10s,
		BackfillSamples: []protocol.BackfillSample{{Name: testDim, Value: 1, TS: now - 3600}},
	}, 512))

	require.Len(t, receiveTelemetryCall(t, writer.calls).samples, 1)
	assert.InDelta(t, 0, promtestutil.ToFloat64(
		m.EdgeTelemetryDropsTotal.WithLabelValues("backfill_out_of_retention")), 0)
	assert.Zero(t, ac.DroppedTelemetryCount())
}
