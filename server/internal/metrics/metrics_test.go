package metrics

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// zeroGaugeSource is a GaugeSource whose every callback reports zero/healthy — a
// base fixture tests override one field at a time. StartGaugeUpdater invokes all
// callbacks unconditionally, so each must be non-nil.
func zeroGaugeSource() GaugeSource {
	return GaugeSource{
		ActiveSessions:      func() int { return 0 },
		ConnectedAgents:     func() int { return 0 },
		ConnectedMPSDevices: func() int { return 0 },
		SignalingSuccesses:  func() int64 { return 0 },
		SignalingFailures:   func() int64 { return 0 },
		RegistryUp:          func() bool { return true },
	}
}

// TestStartGaugeUpdater_RegistryUp asserts the opengate_registry_up gauge is
// registered by NewMetrics and tracks the boolean health reported by the
// GaugeSource: 1 when the session registry is reachable, 0 when not.
func TestStartGaugeUpdater_RegistryUp(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	var up atomic.Bool
	up.Store(true)
	src := zeroGaugeSource()
	src.RegistryUp = up.Load

	go StartGaugeUpdater(t.Context(), m, src, 5*time.Millisecond)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.RegistryUp) == 1
	}, time.Second, 5*time.Millisecond, "gauge should read 1 while the registry is up")

	up.Store(false)
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.RegistryUp) == 0
	}, time.Second, 5*time.Millisecond, "gauge should drop to 0 once the registry is down")
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
