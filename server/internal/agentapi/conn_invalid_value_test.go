package agentapi

import (
	"math"
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

func TestOneValueThatIsNotANumberCostsOnlyItself(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{
			{Name: "cpu.total", Avg: 12.5},
			{Name: "mem.used_percent", Avg: math.NaN()},
			{Name: "disk.used_percent", Avg: math.Inf(1)},
			{Name: "net.rx_bps", Avg: 4096},
		},
	}, 256))

	call := receiveTelemetryCall(t, writer.calls)
	names := make([]string, 0, len(call.samples))
	for _, s := range call.samples {
		names = append(names, s.Labels["dim"])
	}
	assert.Equal(t, []string{"cpu.total", "net.rx_bps"}, names, "the rest of the minute is written")
	assert.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("invalid_value")), 0,
		"one message is one drop however many of its readings were refused")
}

func TestAWindowOfOnlyBadValuesWritesNothing(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, ac.handleAgentMetricWindow(tenantCtx(tenant), &protocol.ControlMessage{
		Type: protocol.MsgAgentMetricWindow,
		TS:   time.Now().Unix(),
		Dims: []protocol.MetricDim{{Name: "cpu.total", Avg: math.NaN()}},
	}, 256))

	assert.Empty(t, writer.calls)
	assert.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("invalid_value")), 0)
}

func TestACatchUpBatchWithOneBadValueIsConfirmedWithTheRestStored(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	ts := time.Now().Unix() - 3600
	require.NoError(t, ac.handleMetricBackfillBatch(tenantCtx(tenant), &protocol.ControlMessage{
		Type: protocol.MsgMetricBackfillBatch,
		Tier: protocol.BackfillTierRecent60s,
		BackfillSamples: []protocol.BackfillSample{
			{Name: "cpu.total", TS: ts, Value: 7},
			{Name: "cpu.total", TS: ts + 60, Value: math.NaN()},
			{Name: "cpu.total", TS: ts + 120, Value: 9},
		},
		Cursor: ts + 120,
	}, 256))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 2)
	assert.InDelta(t, 7, call.samples[0].Value, 0)
	assert.InDelta(t, 9, call.samples[1].Value, 0)
	assert.Equal(t, protocol.MsgMetricBackfillAck, readReply(t, ac, buf).Type,
		"a refused reading counts as filtered, so the batch is confirmed")
	assert.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("invalid_value")), 0)
}
