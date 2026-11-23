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
		{"catch-up answers", m.EdgeBackfillAnswersTotal, BackfillAnswers()},
		{"direct-link outcomes", m.DirectLinkOutcomesTotal, DirectLinkOutcomes()},
		{"helper results", m.HelperRequestsTotal, HelperRequestResults()},
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
	require.InDelta(t, 0, testutil.ToFloat64(m.EdgeTelemetryIngestedTotal.WithLabelValues("DiscoveryReport")), 0)
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

func TestObserveBackfillGrantAndAnswers(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveBackfillGrant(2500)
	m.ObserveBackfillGrant(1800)
	m.ObserveBackfillAnswer("stored", 3)
	m.ObserveBackfillAnswer("deferred_busy", 4)

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("granted")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("stored")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeBackfillAnswersTotal.WithLabelValues("deferred_busy")), 0)
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

func TestDirectLinkOutcomesAndHelperResultsCountUnderTheirOneLabel(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	require.ElementsMatch(t, []string{"direct", "no_route", "unsupported", "dropped"}, DirectLinkOutcomes())
	require.ElementsMatch(t,
		[]string{"answered", "malformed", "address_limit", "total_limit"}, HelperRequestResults())

	m.ObserveDirectLinkOutcome("direct")
	m.ObserveDirectLinkOutcome("direct")
	m.ObserveDirectLinkOutcome("dropped")
	m.ObserveHelperRequest("answered")
	m.ObserveHelperRequest("address_limit")

	for _, c := range []struct {
		counter *prometheus.CounterVec
		label   string
		want    float64
	}{
		{m.DirectLinkOutcomesTotal, "direct", 2},
		{m.DirectLinkOutcomesTotal, "dropped", 1},
		{m.HelperRequestsTotal, "answered", 1},
		{m.HelperRequestsTotal, "address_limit", 1},
	} {
		require.InDelta(t, c.want, testutil.ToFloat64(c.counter.WithLabelValues(c.label)), 0, c.label)
	}
	require.Equal(t, 4, testutil.CollectAndCount(m.DirectLinkOutcomesTotal),
		"four series: the outcome is the only label")
}

// instanceIDsOn gathers the registry and returns the instance_id label of each info sample.
func instanceIDsOn(t *testing.T, reg *prometheus.Registry) []string {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	var ids []string
	for _, family := range families {
		if family.GetName() != "opengate_process_instance_info" {
			continue
		}
		for _, sample := range family.GetMetric() {
			require.InDelta(t, 1, sample.GetGauge().GetValue(), 0)
			for _, label := range sample.GetLabel() {
				if label.GetName() == "instance_id" {
					ids = append(ids, label.GetValue())
				}
			}
		}
	}
	return ids
}

func TestTheRegistryNamesThisProcessByOneIDMadeAtItsStart(t *testing.T) {
	first := instanceIDsOn(t, NewRegistry())
	second := instanceIDsOn(t, NewRegistry())

	require.Len(t, first, 1, "one process publishes one instance")
	require.GreaterOrEqual(t, len(first[0]), 26, "the ID carries 128 random bits")
	require.Equal(t, first, second, "every registry in one process names the same start")
}
