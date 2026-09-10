package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A phase declares a level and a length, and the two together are an offer: a
// step going from eight thousand machines to sixteen thousand over five minutes
// is offering fifty-three arrivals a second. The fleet is what makes that true.
//
// It did not. Every machine a step added was dialled the instant the step was
// asked for, so the offer a profile declared arrived as ten bursts — and the
// server, which refuses enrolments past a hundred a second on purpose, turned
// most of each burst away. The volume family's eight-thousand leg reached 34% of
// its declared arrival rate and the breakpoint ladder's top step reached 21%,
// both on a target that was never asked to carry the load at all.
func TestTheClimbIsSpreadOverTheWindowItIsGiven(t *testing.T) {
	t.Run("a window spreads the dialling across it", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		began := time.Now()
		require.NoError(t, fleet.HoldConnected(400*time.Millisecond, 20))

		// The level is the run's own bookkeeping and it is true at once, so the
		// step after this one asks for the level it was going to ask for.
		assert.Equal(t, 20, fleet.Connected())
		// The dialling is not. A burst would have every machine away before
		// this line runs.
		assert.Less(t, starter.startedCount(), 20)

		require.Eventually(t, func() bool { return starter.startedCount() == 20 },
			5*time.Second, 5*time.Millisecond)
		// And it took about the window it was given rather than no time at all,
		// which is the whole of the difference between an offer and a burst.
		assert.GreaterOrEqual(t, time.Since(began), 300*time.Millisecond)
	})

	t.Run("no window dials at once", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 5))

		// A wind-down and a fleet with no time to spread over both land here,
		// and neither should be made to wait for a window nobody declared.
		require.Eventually(t, func() bool { return starter.startedCount() == 5 },
			5*time.Second, 5*time.Millisecond)
	})

	t.Run("a machine let go before it dialled is neither an arrival nor a failure", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)

		// A long window against a level the run immediately abandons: every
		// machine is still waiting its turn when the fleet is wound down.
		require.NoError(t, fleet.HoldConnected(time.Hour, 50))
		fleet.Stop()

		// Counting these as machines that failed to arrive would report an
		// error rate for a phase that never offered them.
		assert.Zero(t, starter.startedCount(), "none of them ever dialled")
		outcomes := fleet.Outcomes()
		assert.Zero(t, outcomes.Arrived)
		assert.Zero(t, outcomes.Failed)
		assert.Empty(t, fleet.Results())
	})

	t.Run("winding down needs no window", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 6))
		require.Eventually(t, func() bool { return starter.startedCount() == 6 },
			5*time.Second, 5*time.Millisecond)

		began := time.Now()
		require.NoError(t, fleet.HoldConnected(time.Minute, 2))
		assert.Equal(t, 2, fleet.Connected())
		// A machine leaving is not an arrival, so nothing about it is paced.
		assert.Less(t, time.Since(began), 10*time.Second)
	})
}

// The sequencer is what hands the fleet its window, and the window is the gap
// until the next step of the climb. A phase that handed the fleet nothing would
// leave every step a burst however carefully the fleet paced.
func TestThePhaseHandsTheFleetTheGapUntilItsNextStep(t *testing.T) {
	profile := &Profile{
		Name:   "paced",
		Family: "normal",
		Phases: []Phase{{
			Name:            "climb",
			Duration:        Duration{Duration: 100 * time.Second},
			ConnectedAgents: 100,
		}},
	}
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	_, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	require.Len(t, fleet.steps, rampSteps)
	for _, step := range fleet.steps {
		assert.Equal(t, 10*time.Second, step.within,
			"each step has the phase's length divided by the steps to climb in")
	}
}
