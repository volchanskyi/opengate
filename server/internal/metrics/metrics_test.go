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
