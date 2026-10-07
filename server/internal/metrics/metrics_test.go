package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestObserveDeviceLogPull(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveDeviceLogPull("ok", 20*time.Millisecond)
	m.ObserveDeviceLogPull("ok", 30*time.Millisecond)
	m.ObserveDeviceLogPull("timeout", 15*time.Second)

	require.InDelta(t, 2, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues("ok")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues("timeout")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues("busy")), 0)
	require.Equal(t, 2, testutil.CollectAndCount(m.DeviceLogPullDuration))
}

func TestEveryLogPullOutcomeStartsAtZero(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	require.Equal(t, len(DeviceLogPullOutcomes()), testutil.CollectAndCount(m.DeviceLogPullsTotal),
		"every outcome is published before any pull")
	for _, outcome := range DeviceLogPullOutcomes() {
		require.InDelta(t, 0, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues(outcome)), 0, outcome)
	}
	require.Zero(t, testutil.CollectAndCount(m.DeviceLogPullDuration), "no pull, no latency")
}

func TestEveryEdgeTelemetryOutcomeStartsAtZero(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	for _, c := range []struct {
		name    string
		counter *prometheus.CounterVec
		values  []string
	}{
		{"drop reasons", m.EdgeTelemetryDropsTotal, EdgeTelemetryDropReasons()},
		{"ingested message types", m.EdgeTelemetryIngestedTotal, EdgeTelemetryIngestTypes()},
		{"catch-up decisions", m.EdgeBackfillDecisionsTotal, []string{"grant", "defer"}},
	} {
		require.NotEmpty(t, c.values, c.name)
		require.Equal(t, len(c.values), testutil.CollectAndCount(c.counter),
			"every one of the %s is published before anything happens", c.name)
		for _, value := range c.values {
			require.InDelta(t, 0, testutil.ToFloat64(c.counter.WithLabelValues(value)), 0, value)
		}
	}
}

func TestObserveAgentTLSHandshake(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	require.InDelta(t, 0, testutil.ToFloat64(m.AgentTLSHandshakesTotal.WithLabelValues("true")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.AgentTLSHandshakesTotal.WithLabelValues("false")), 0)
	require.Equal(t, 2, testutil.CollectAndCount(m.AgentTLSHandshakesTotal),
		"both label values are published before either is observed")

	m.ObserveAgentTLSHandshake(false)
	m.ObserveAgentTLSHandshake(true)
	m.ObserveAgentTLSHandshake(true)

	require.InDelta(t, 2, testutil.ToFloat64(m.AgentTLSHandshakesTotal.WithLabelValues("true")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.AgentTLSHandshakesTotal.WithLabelValues("false")), 0)
}

func TestObserveEdgeTelemetryIngest(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveEdgeTelemetryIngest("AgentMetricWindow")
	m.ObserveEdgeTelemetryIngest("AgentMetricWindow")
	m.ObserveEdgeTelemetryIngest("AgentHealthSummary")

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeTelemetryIngestedTotal.WithLabelValues("AgentMetricWindow")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryIngestedTotal.WithLabelValues("AgentHealthSummary")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.EdgeTelemetryIngestedTotal.WithLabelValues("ProcessReport")), 0)
}

func TestObserveEdgeTelemetryDrop(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveEdgeTelemetryDrop("interval_floor", 1)
	m.ObserveEdgeTelemetryDrop("interval_floor", 1)
	m.ObserveEdgeTelemetryDrop("persist_slots_full", 1)
	m.ObserveEdgeTelemetryDrop("persist_failed", 6)

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("interval_floor")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("persist_slots_full")), 0)
	require.InDelta(t, 6, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("persist_failed")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("payload_too_large")), 0)
}

func TestObserveEdgeTelemetryClockClamp(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveEdgeTelemetryClockClamp("future")
	m.ObserveEdgeTelemetryClockClamp("future")
	m.ObserveEdgeTelemetryClockClamp("past")

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeTelemetryClockClampedTotal.WithLabelValues("future")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryClockClampedTotal.WithLabelValues("past")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("clock_skew_clamped")), 0)
}

func TestObserveBackfillDecision(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveBackfillDecision(true, 2500, 3)
	m.ObserveBackfillDecision(false, 0, 3)
	m.ObserveBackfillDecision(true, 1800, 4)

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeBackfillDecisionsTotal.WithLabelValues("grant")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeBackfillDecisionsTotal.WithLabelValues("defer")), 0)
	require.InDelta(t, 4, testutil.ToFloat64(m.EdgeBackfillActiveSlots), 0)
	require.InDelta(t, 1800, testutil.ToFloat64(m.EdgeBackfillGrantRate), 0)
}

type dbSizerFunc func(context.Context) (int64, error)

func (f dbSizerFunc) Size(ctx context.Context) (int64, error) { return f(ctx) }

func TestStartDBSizeUpdaterPreservesGaugeOnError(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	m.DBSizeBytes.Set(42)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	StartDBSizeUpdater(ctx, m, dbSizerFunc(func(context.Context) (int64, error) {
		return 99, errors.New("size unavailable")
	}), discardLogger(), time.Hour)

	require.InDelta(t, 42, testutil.ToFloat64(m.DBSizeBytes), 0)
}
