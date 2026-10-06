package agentapi

import (
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

func TestTelemetryIngestIsCounted(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	msg := &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 12.5}},
	}
	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), msg, 256))
	ac.flushTelemetry(tenantCtx(tenant))

	<-writer.calls
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryIngestedTotal.WithLabelValues(string(protocol.MsgAgentMetricWindow))), 0)
}

func TestTelemetryDropIsCounted(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	msg := &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 1}},
	}
	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), msg, maxTelemetryPayloadBytes+1))

	assert.Empty(t, writer.calls)
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("payload_too_large")), 0)
}

func TestBackfillDecisionIsObserved(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	ac, _ := backfillConn(t, s, uuid.New(), true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type:           protocol.MsgRequestBackfillSlot,
		PendingSamples: 5000,
	}))

	assert.InDelta(t, 1, testutil.ToFloat64(m.EdgeBackfillDecisionsTotal.WithLabelValues("grant")), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.EdgeBackfillActiveSlots), 0)
	assert.GreaterOrEqual(t, testutil.ToFloat64(m.EdgeBackfillGrantRate), float64(schedCfg().MinGrantRate))
}

func TestNilMetricsIsSafe(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	require.Nil(t, ac.metrics)

	msg := &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: 1}},
	}
	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), msg, 256))
	ac.flushTelemetry(tenantCtx(tenant))
	<-writer.calls
}
