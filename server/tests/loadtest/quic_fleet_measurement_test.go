package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMachineThatCompletesLeavesTheConnectedCount(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int64
	fleet := NewQUICFleet(func(ctx context.Context, _ int, presence fleetPresence) agentResult {
		started.Add(1)
		presence.Arrived()
		select {
		case <-release:
		case <-ctx.Done():
		}
		presence.Left()
		return agentResult{connectDur: 5 * time.Millisecond}
	})
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 4))
	require.Eventually(t, func() bool { return started.Load() == 4 },
		2*time.Second, 10*time.Millisecond)
	require.Equal(t, 4, fleet.Connected())

	close(release)

	require.Eventually(t, func() bool { return fleet.Connected() == 0 },
		2*time.Second, 10*time.Millisecond,
		"a machine that has finished is not one of the connected")
}

func TestTheFleetCountsWhatIsUpRatherThanWhatWasStarted(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)

	require.NoError(t, fleet.HoldConnected(0, 3))
	require.Eventually(t, func() bool { return starter.startedCount() == 3 },
		2*time.Second, 10*time.Millisecond)

	fleet.Stop()
	assert.Equal(t, 0, fleet.Connected())
}

func TestTheFleetTalliesWhatEachMachineSaw(t *testing.T) {
	outcomes := []agentResult{
		{connectDur: time.Millisecond, arrivedAt: time.Now()},
		{connectDur: time.Millisecond, arrivedAt: time.Now()},
		{err: errors.New("dial: timeout")},
		{err: ErrHeldPeerGone},
		{err: ErrEnrollmentRefused},
	}
	var next atomic.Int64
	fleet := NewQUICFleet(func(_ context.Context, _ int, presence fleetPresence) agentResult {
		result := outcomes[next.Add(1)-1]
		if !result.arrivedAt.IsZero() {
			presence.Arrived()
		}
		return result
	})

	require.NoError(t, fleet.HoldConnected(0, len(outcomes)))
	fleet.Stop()

	tally := fleet.Outcomes()
	assert.EqualValues(t, 2, tally.Arrived)
	assert.EqualValues(t, 2, tally.Failed,
		"two machines did not get in; the third was refused by a limit doing its job")
	assert.EqualValues(t, 1, tally.Severed, "a machine whose held connection went away is a fault")
	assert.EqualValues(t, 1, tally.Rejected, "a refusal the server made on purpose is the system working")
	assert.EqualValues(t, 4, tally.Attempted(),
		"and a refusal leaves the denominator with the numerator, or the share it produces is of a fleet that was never asked")
}

func TestProbeLatencyIsALiveRoundTrip(t *testing.T) {
	var probes atomic.Int64
	fleet := NewQUICFleetWithProbe(
		func(ctx context.Context, _ int, presence fleetPresence) agentResult {
			<-ctx.Done()
			return agentResult{}
		},
		func(context.Context) (time.Duration, error) {
			probes.Add(1)
			return 37 * time.Millisecond, nil
		})
	defer fleet.Stop()

	assert.Equal(t, 37*time.Millisecond, fleet.ProbeLatency())
	assert.EqualValues(t, 1, probes.Load())
}

func TestAProbeThatFailsReportsNoLatency(t *testing.T) {
	fleet := NewQUICFleetWithProbe(
		func(ctx context.Context, _ int, presence fleetPresence) agentResult {
			<-ctx.Done()
			return agentResult{}
		},
		func(context.Context) (time.Duration, error) {
			return 0, errors.New("dial: connection refused")
		})
	defer fleet.Stop()

	assert.Zero(t, fleet.ProbeLatency())
}

func TestAFleetWithNoProberReportsNoLatency(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 2))
	assert.Zero(t, fleet.ProbeLatency())
}

func TestArrivalsAreCountedWhileTheMachinesAreStillHeld(t *testing.T) {
	fleet := NewQUICFleet(func(ctx context.Context, _ int, presence fleetPresence) agentResult {
		presence.Arrived()
		<-ctx.Done()
		presence.Left()
		return agentResult{connectDur: time.Millisecond}
	})
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 3))
	require.Eventually(t, func() bool { return fleet.Outcomes().Arrived == 3 },
		2*time.Second, 10*time.Millisecond,
		"three machines that arrived and are still connected are three arrivals")
	assert.EqualValues(t, 0, fleet.Outcomes().Failed,
		"a machine that has not ended has not failed")
}

func TestAMachineThatNeverArrivedIsCountedOnceAsAFailure(t *testing.T) {
	fleet := NewQUICFleet(func(context.Context, int, fleetPresence) agentResult {
		return agentResult{err: errors.New("dial: timeout")}
	})
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 2))
	require.Eventually(t, func() bool { return fleet.Outcomes().Failed == 2 },
		2*time.Second, 10*time.Millisecond)
	assert.EqualValues(t, 0, fleet.Outcomes().Arrived)
	assert.EqualValues(t, 2, fleet.Outcomes().Attempted(),
		"every machine produces exactly one outcome")
}

func TestAnArrivedMachineThatIsSeveredIsAFaultRatherThanAFailedArrival(t *testing.T) {
	fleet := NewQUICFleet(func(_ context.Context, _ int, presence fleetPresence) agentResult {
		presence.Arrived()
		return agentResult{err: ErrHeldPeerGone}
	})
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 1))
	require.Eventually(t, func() bool { return fleet.Outcomes().Severed == 1 },
		2*time.Second, 10*time.Millisecond)

	tally := fleet.Outcomes()
	assert.EqualValues(t, 1, tally.Arrived)
	assert.EqualValues(t, 0, tally.Failed, "a machine that arrived did not fail to arrive")
	assert.InDelta(t, 0.0, tally.ErrorRate(), 0.001)
}
