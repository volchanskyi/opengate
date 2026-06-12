package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
