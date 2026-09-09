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

// What the fleet knows about itself, and what it had been reporting instead.

// D9. A machine left the connected set only when it errored. One that finished
// its hold normally stayed counted for the life of the run, so the fleet's
// count was a count of machines started — and a bundle saying 500 connected
// said only that 500 were once dialled.
func TestAMachineThatCompletesLeavesTheConnectedCount(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int64
	fleet := NewQUICFleet(func(ctx context.Context, _ int, arrived func()) agentResult {
		started.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return agentResult{connectDur: 5 * time.Millisecond}
	})
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 4))
	require.Eventually(t, func() bool { return started.Load() == 4 },
		2*time.Second, 10*time.Millisecond)
	require.Equal(t, 4, fleet.Connected())

	// Every machine finishes of its own accord, which is what a machine whose
	// hold has elapsed does. Nothing errored and nothing was wound down.
	close(release)

	require.Eventually(t, func() bool { return fleet.Connected() == 0 },
		2*time.Second, 10*time.Millisecond,
		"a machine that has finished is not one of the connected")
}

// The gap between what was asked for and what is there is the finding, so a
// machine that completed must not still be filling a slot the level would
// otherwise refill.
func TestTheFleetCountsWhatIsUpRatherThanWhatWasStarted(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)

	require.NoError(t, fleet.HoldConnected(0, 3))
	require.Eventually(t, func() bool { return starter.startedCount() == 3 },
		2*time.Second, 10*time.Millisecond)

	fleet.Stop()
	assert.Equal(t, 0, fleet.Connected())
}

// The tallies a phase divides into its error rate, its faults and its expected
// rejections. Each is counted where it is known — at the machine that saw it.
func TestTheFleetTalliesWhatEachMachineSaw(t *testing.T) {
	outcomes := []agentResult{
		{connectDur: time.Millisecond, arrivedAt: time.Now()},
		{connectDur: time.Millisecond, arrivedAt: time.Now()},
		{err: errors.New("dial: timeout")},
		{err: ErrHeldPeerGone},
		{err: ErrEnrollmentRefused},
	}
	var next atomic.Int64
	fleet := NewQUICFleet(func(_ context.Context, _ int, arrived func()) agentResult {
		result := outcomes[next.Add(1)-1]
		// A machine says so when it reaches registered, which the two that
		// carry an arrival time did and the three that carry an error did not.
		if !result.arrivedAt.IsZero() {
			arrived()
		}
		return result
	})

	require.NoError(t, fleet.HoldConnected(0, len(outcomes)))
	fleet.Stop()

	tally := fleet.Outcomes()
	assert.EqualValues(t, 2, tally.Arrived)
	assert.EqualValues(t, 3, tally.Failed, "three machines never reached registered")
	assert.EqualValues(t, 1, tally.Severed, "a machine whose held connection went away is a fault")
	assert.EqualValues(t, 1, tally.Rejected, "a refusal the server made on purpose is the system working")
}

// D10. The phase's latency was the last finished machine's connect time, which
// in a profiled run is no machine at all. A live round trip is a fresh connect,
// handshake and register — the only one the machine side has, because the
// control stream has no reply to a heartbeat.
func TestProbeLatencyIsALiveRoundTrip(t *testing.T) {
	var probes atomic.Int64
	fleet := NewQUICFleetWithProbe(
		func(ctx context.Context, _ int, arrived func()) agentResult {
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

// A round trip that could not be taken is absent rather than instant: zero is
// the fastest reading ever recorded, and this is the opposite of one.
func TestAProbeThatFailsReportsNoLatency(t *testing.T) {
	fleet := NewQUICFleetWithProbe(
		func(ctx context.Context, _ int, arrived func()) agentResult {
			<-ctx.Done()
			return agentResult{}
		},
		func(context.Context) (time.Duration, error) {
			return 0, errors.New("dial: connection refused")
		})
	defer fleet.Stop()

	assert.Zero(t, fleet.ProbeLatency())
}

// A fleet built without a prober takes no round trips rather than reporting a
// stale one, which is what the field used to carry.
func TestAFleetWithNoProberReportsNoLatency(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 2))
	assert.Zero(t, fleet.ProbeLatency())
}

// D6. An arrival was tallied when a machine's life *ended*, and in a profiled
// run no machine's life ends inside a phase — the fleet is held to the end of
// the walk. So every phase of every profiled run reported nought arrivals
// against an offered rate it had genuinely met, and the floor that invalidates
// a run for load never offered fired on all five legs of a sweep whose fleets
// had all arrived. An arrival is counted where it happens.
func TestArrivalsAreCountedWhileTheMachinesAreStillHeld(t *testing.T) {
	fleet := NewQUICFleet(func(ctx context.Context, _ int, arrived func()) agentResult {
		arrived()
		<-ctx.Done()
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

// A machine that never reached registered is the failure, and it is counted
// once — when its life ends, which is the first moment anything knows.
func TestAMachineThatNeverArrivedIsCountedOnceAsAFailure(t *testing.T) {
	fleet := NewQUICFleet(func(context.Context, int, func()) agentResult {
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

// A machine that arrived and was later cut off is a fault rather than a
// failure to arrive. Counting it as both puts one machine into the attempted
// tally twice and reports an error rate for a phase whose every machine turned
// up.
func TestAnArrivedMachineThatIsSeveredIsAFaultRatherThanAFailedArrival(t *testing.T) {
	fleet := NewQUICFleet(func(_ context.Context, _ int, arrived func()) agentResult {
		arrived()
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
