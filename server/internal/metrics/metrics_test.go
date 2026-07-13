package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// zeroGaugeSource is a GaugeSource whose every callback reports zero.
func zeroGaugeSource() GaugeSource {
	return GaugeSource{
		ActiveSessions:      func() int { return 0 },
		ConnectedAgents:     func() int { return 0 },
		ConnectedMPSDevices: func() int { return 0 },
		SignalingSuccesses:  func() int64 { return 0 },
		SignalingFailures:   func() int64 { return 0 },
	}
}

// TestObserveDeviceLogPull records raw-log broker pulls against the pull-count
// and pull-duration metrics, keyed by outcome. The ok count is the audited
// pull count (each ok pull writes exactly one device.logs.read audit event).
func TestObserveDeviceLogPull(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveDeviceLogPull("ok", 20*time.Millisecond)
	m.ObserveDeviceLogPull("ok", 30*time.Millisecond)
	m.ObserveDeviceLogPull("timeout", 15*time.Second)

	require.InDelta(t, 2, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues("ok")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues("timeout")), 0)
	// A distinct result label that was never observed stays at zero.
	require.InDelta(t, 0, testutil.ToFloat64(m.DeviceLogPullsTotal.WithLabelValues("busy")), 0)
	// The duration histogram has one series per observed outcome (ok, timeout).
	require.Equal(t, 2, testutil.CollectAndCount(m.DeviceLogPullDuration))
}

// TestObserveEdgeTelemetryIngest counts accepted Edge-Sentinel telemetry
// messages by control type, so the soak dashboard can chart ingest rate.
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

// TestObserveEdgeTelemetryDrop counts dropped telemetry by reason so the soak
// dashboard can chart drop count and break it down by cause.
func TestObserveEdgeTelemetryDrop(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveEdgeTelemetryDrop("interval_floor")
	m.ObserveEdgeTelemetryDrop("interval_floor")
	m.ObserveEdgeTelemetryDrop("persist_slots_full")

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("interval_floor")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("persist_slots_full")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("payload_too_large")), 0)
}

// TestObserveBackfillDecision records the reconnect-backfill scheduler's
// grant/defer decisions, the granted per-slot rate, and the live active-slot
// count, so the soak dashboard can chart scheduler state during a storm.
func TestObserveBackfillDecision(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveBackfillDecision(true, 2500, 3)
	m.ObserveBackfillDecision(false, 0, 3)
	m.ObserveBackfillDecision(true, 1800, 4)

	require.InDelta(t, 2, testutil.ToFloat64(m.EdgeBackfillDecisionsTotal.WithLabelValues("grant")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.EdgeBackfillDecisionsTotal.WithLabelValues("defer")), 0)
	// Active slots reflect the most recent observation.
	require.InDelta(t, 4, testutil.ToFloat64(m.EdgeBackfillActiveSlots), 0)
	// The granted-rate gauge reflects the most recent grant's rate; a defer
	// leaves it unchanged.
	require.InDelta(t, 1800, testutil.ToFloat64(m.EdgeBackfillGrantRate), 0)
}

// TestStartGaugeUpdater_StopsOnCancel verifies the updater returns when its
// context is cancelled rather than leaking the ticker goroutine.
func TestStartGaugeUpdater_StopsOnCancel(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		StartGaugeUpdater(ctx, m, zeroGaugeSource(), time.Millisecond)
		close(done)
	}()

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond, "updater should return after context cancellation")
}
