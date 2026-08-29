package metrics

import (
	"context"
	"errors"
	"sync/atomic"
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

// TestObserveAgentTLSHandshake counts every agent QUIC connection that reached
// the application handshake, split by whether TLS resumed. Both series exist
// from start-up: the resumption ratio divides one by their sum, and a missing
// denominator reads as "no data" exactly when somebody is checking whether
// reconnects are resuming at all.
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
// dashboard can chart drop count and break it down by cause. A discarded
// coalesced batch reports every message it carried in one call, so the drop
// count stays comparable with the ingest count.
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

// TestObserveEdgeTelemetryClockClamp counts corrected agent clocks by direction.
// It is a separate counter from drops because a clamped message is still
// persisted; folding it into drops would break the ingest ledger.
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

func TestStartGaugeUpdaterTracksSignalingDeltas(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var successes atomic.Int64
	var failures atomic.Int64
	successes.Store(5)
	failures.Store(3)
	src := zeroGaugeSource()
	src.SignalingSuccesses = successes.Load
	src.SignalingFailures = failures.Load

	done := make(chan struct{})
	go func() {
		StartGaugeUpdater(ctx, m, src, time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.SignalingUpgradesTotal.WithLabelValues("success")) == 5 &&
			testutil.ToFloat64(m.SignalingUpgradesTotal.WithLabelValues("failure")) == 3
	}, time.Second, 5*time.Millisecond)

	successes.Store(8)
	failures.Store(7)
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.SignalingUpgradesTotal.WithLabelValues("success")) == 8 &&
			testutil.ToFloat64(m.SignalingUpgradesTotal.WithLabelValues("failure")) == 7
	}, time.Second, 5*time.Millisecond)

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
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
