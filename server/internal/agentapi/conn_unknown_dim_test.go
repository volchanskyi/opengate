package agentapi

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// A window carrying an unlisted dim persists the listed ones and nothing else,
// and says so through the drop counter rather than silently.
func TestMetricWindowDropsUnlistedDims(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	msg := &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{
			{Name: "cpu.total", Avg: 12.5},
			{Name: "cpu.total.p99", Avg: 99.0},
			{Name: "cpu.total.max", Avg: 100.0},
		},
	}
	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), msg, 256))
	ac.flushTelemetry(tenantCtx(tenant))

	call := <-writer.calls
	names := make([]string, 0, len(call.samples))
	for _, s := range call.samples {
		names = append(names, s.Labels["dim"])
	}
	assert.Equal(t, []string{"cpu.total", "cpu.total.max"}, names,
		"only the agreed vocabulary is written")
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_dim")), 0)
}

// The cardinality argument in one test: a window of a thousand invented dims
// creates no series at all, so a misbehaving agent cannot enlarge the central
// store. Without the allowlist every one of these would have become a label.
func TestMetricWindowOfJunkDimsWritesNothing(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	dims := make([]protocol.MetricDim, 0, 1000)
	for i := range 1000 {
		dims = append(dims, protocol.MetricDim{Name: fmt.Sprintf("attacker.dim.%d", i), Avg: 1})
	}
	msg := &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: dims,
	}
	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), msg, 256))
	ac.flushTelemetry(tenantCtx(tenant))

	assert.Empty(t, writer.calls, "a window of invented dims reaches no store")
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_dim")), 0)
}

// Backfill writes the same central series, so it answers to the same
// vocabulary — otherwise the allowlist would close the live path and leave the
// replay path open.
func TestBackfillBatchDropsUnlistedDims(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m
	now := time.Now().Unix()

	msg := &protocol.ControlMessage{
		Type: protocol.MsgMetricBackfillBatch,
		Tier: protocol.BackfillTierRecent60s,
		BackfillSamples: []protocol.BackfillSample{
			{Name: "mem.used_percent", TS: now - 120, Value: 41},
			{Name: "mem.used_percent.p95", TS: now - 120, Value: 88},
		},
		Cursor: now - 120,
	}
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), msg, 256))

	call := <-writer.calls
	require.Len(t, call.samples, 1)
	assert.Equal(t, "mem.used_percent", call.samples[0].Labels["dim"])
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_dim")), 0)
}
