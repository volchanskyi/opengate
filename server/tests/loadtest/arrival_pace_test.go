package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheClimbIsSpreadOverTheWindowItIsGiven(t *testing.T) {
	t.Run("a window spreads the dialling across it", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		began := time.Now()
		require.NoError(t, fleet.HoldConnected(400*time.Millisecond, 20))

		assert.Less(t, starter.startedCount(), 20)

		// Asking again for the level already asked for adds no machines while the climb runs.
		require.NoError(t, fleet.HoldConnected(0, 20))

		require.Eventually(t, func() bool { return starter.startedCount() == 20 },
			5*time.Second, 5*time.Millisecond)
		assert.Equal(t, 20, starter.startedCount(), "twenty asked for, twenty dialled")
		assert.GreaterOrEqual(t, time.Since(began), 300*time.Millisecond)
	})

	t.Run("no window dials at once", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 5))

		require.Eventually(t, func() bool { return starter.startedCount() == 5 },
			5*time.Second, 5*time.Millisecond)
	})

	t.Run("a machine let go before it dialled is neither an arrival nor a failure", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)

		require.NoError(t, fleet.HoldConnected(time.Hour, 50))
		fleet.Stop()

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
		assert.Less(t, time.Since(began), 10*time.Second)
		require.Eventually(t, func() bool { return fleet.Connected() == 2 },
			5*time.Second, 5*time.Millisecond)
	})
}

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
