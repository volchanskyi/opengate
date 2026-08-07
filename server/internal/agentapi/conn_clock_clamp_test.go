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

// TestClampTelemetryTimestamp pins the accepted clock window: a wild-future
// stamp is pulled back to the skew ceiling, a stale one up to the backlog floor,
// and anything inside both bounds is left exactly as the agent sent it.
func TestClampTelemetryTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()

	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
		dir  string
	}{
		{"seven hours ahead clamps to the skew ceiling", 7 * time.Hour, maxTelemetrySkew, clampFuture},
		{"eight days behind clamps to the backlog floor", -8 * 24 * time.Hour, -maxTelemetryBacklog, clampPast},
		{"six days behind is untouched", -6 * 24 * time.Hour, -6 * 24 * time.Hour, ""},
		{"the skew ceiling itself is untouched", maxTelemetrySkew, maxTelemetrySkew, ""},
		{"the backlog floor itself is untouched", -maxTelemetryBacklog, -maxTelemetryBacklog, ""},
		{"one second past the ceiling clamps", maxTelemetrySkew + time.Second, maxTelemetrySkew, clampFuture},
		{"one second past the floor clamps", -maxTelemetryBacklog - time.Second, -maxTelemetryBacklog, clampPast},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dir := clampTelemetryTimestamp(now.Add(tt.in).Unix(), now)
			assert.Equal(t, now.Add(tt.want), got)
			assert.Equal(t, tt.dir, dir)
		})
	}

	got, dir := clampTelemetryTimestamp(0, now)
	assert.Equal(t, now, got, "a missing timestamp takes the server clock")
	assert.Empty(t, dir)
}

// TestClampTelemetryTimestampPreservesOrder pins monotonicity: clamping may
// collapse out-of-window stamps onto a bound, but it never reorders a batch.
func TestClampTelemetryTimestampPreservesOrder(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	offsets := []time.Duration{
		9 * time.Hour, -30 * time.Minute, -9 * 24 * time.Hour, time.Minute, -2 * time.Hour,
	}
	clamped := make([]time.Time, len(offsets))
	for i, off := range offsets {
		clamped[i], _ = clampTelemetryTimestamp(now.Add(off).Unix(), now)
	}
	for i := range offsets {
		for j := range offsets {
			if offsets[i] <= offsets[j] {
				assert.False(t, clamped[i].After(clamped[j]),
					"clamping reordered %v and %v", offsets[i], offsets[j])
			}
		}
	}
}

// TestClockClampIsCountedAndStillPersisted pins the distinction the ledger
// depends on: a clamped message is corrected, not discarded — it lands on its
// own counter with a direction label and is still written.
func TestClockClampIsCountedAndStillPersisted(t *testing.T) {
	tests := []struct {
		name      string
		offset    time.Duration
		want      time.Duration
		direction string
	}{
		{"host clock hours ahead", 7 * time.Hour, maxTelemetrySkew, clampFuture},
		{"host clock days behind", -8 * 24 * time.Hour, -maxTelemetryBacklog, clampPast},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenant := uuid.New()
			writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
			ac, _ := ingestConn(t, tenant, writer, true)
			m := appmetrics.NewMetrics(prometheus.NewRegistry())
			ac.metrics = m

			ctx := tenantCtx(tenant)
			stamped := time.Now().Add(tt.offset).Unix()
			require.NoError(t, ac.handleAgentMetricWindow(ctx, metricWindowMsg(stamped,
				protocol.MetricDim{Name: testDim, Avg: 1}), 256))
			ac.flushTelemetry(ctx)

			call := receiveTelemetryCall(t, writer.calls)
			require.Len(t, call.samples, 1, "a clamped message is still persisted")
			assert.WithinDuration(t, time.Now().UTC().Add(tt.want), call.samples[0].TS, time.Minute)
			assert.InDelta(t, 1, promtestutil.ToFloat64(
				m.EdgeTelemetryClockClampedTotal.WithLabelValues(tt.direction)), 0)
			assert.Zero(t, ac.DroppedTelemetryCount(), "clamped is not dropped")
		})
	}
}

// TestClockClampCountsOnlyPersistedMessages pins that a message discarded for
// carrying nothing to store is reported once, as a drop — its timestamp was
// never written, so the clamp counter must not report it too.
func TestClockClampCountsOnlyPersistedMessages(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleAgentHealthSummary(tenantCtx(tenant), &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary,
		TS:   time.Now().Add(7 * time.Hour).Unix(),
	}, 256))

	assert.Empty(t, writer.calls)
	assert.InDelta(t, 1, promtestutil.ToFloat64(
		m.EdgeTelemetryDropsTotal.WithLabelValues("empty_summary")), 0)
	assert.InDelta(t, 0, promtestutil.ToFloat64(
		m.EdgeTelemetryClockClampedTotal.WithLabelValues(clampFuture)), 0)
}

// TestHealthWindowClampPreservesSummaryOrder drives a batch of out-of-order
// summaries — two of them outside the window — through the real handler and
// pins that the persisted samples keep the order the agent sent them in.
func TestHealthWindowClampPreservesSummaryOrder(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	now := time.Now()
	offsets := []time.Duration{
		-9 * 24 * time.Hour, -time.Hour, 9 * time.Hour, -2 * time.Hour, -time.Minute,
	}
	msg := &protocol.ControlMessage{Type: protocol.MsgHealthWindowResponse}
	for i, off := range offsets {
		msg.Summaries = append(msg.Summaries, protocol.HealthSummary{
			TS:              now.Add(off).Unix(),
			NodeAnomalyRate: float64(i),
			SamplerVersion:  "s1",
		})
	}

	ctx := tenantCtx(tenant)
	require.NoError(t, ac.handleHealthWindowResponse(ctx, msg, 512))
	ac.flushTelemetry(ctx)

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, len(offsets))
	for i, s := range call.samples {
		assert.InDelta(t, float64(i), s.Value, 0, "sample %d left its input position", i)
	}
	for i := range offsets {
		for j := range offsets {
			if offsets[i] <= offsets[j] {
				assert.False(t, call.samples[i].TS.After(call.samples[j].TS),
					"clamping reordered summaries %d and %d", i, j)
			}
		}
	}
	assert.InDelta(t, 1, promtestutil.ToFloat64(
		m.EdgeTelemetryClockClampedTotal.WithLabelValues(clampPast)), 0)
	assert.InDelta(t, 1, promtestutil.ToFloat64(
		m.EdgeTelemetryClockClampedTotal.WithLabelValues(clampFuture)), 0)
}
