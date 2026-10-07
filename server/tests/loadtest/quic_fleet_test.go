package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCountedFleet(t *testing.T) (*startCounter, *QUICFleet) {
	t.Helper()
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	t.Cleanup(fleet.Stop)
	return starter, fleet
}

func awaitStarted(t *testing.T, starter *startCounter, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return starter.startedCount() == want },
		2*time.Second, 10*time.Millisecond)
}

func TestFleetHoldsTheLevelItIsAskedFor(t *testing.T) {
	t.Run("climbs to the level", func(t *testing.T) {
		starter, fleet := newCountedFleet(t)

		require.NoError(t, fleet.HoldConnected(0, 5))

		awaitStarted(t, starter, 5)
		awaitConnected(t, fleet, 5)
	})

	t.Run("climbs again without restarting what is already up", func(t *testing.T) {
		starter, fleet := newCountedFleet(t)

		require.NoError(t, fleet.HoldConnected(0, 3))
		require.NoError(t, fleet.HoldConnected(time.Second, 8))

		awaitStarted(t, starter, 8)
		awaitConnected(t, fleet, 8)
	})

	t.Run("holding the level it already holds starts nothing", func(t *testing.T) {
		starter, fleet := newCountedFleet(t)

		require.NoError(t, fleet.HoldConnected(0, 3))
		require.NoError(t, fleet.HoldConnected(time.Second, 3))

		awaitStarted(t, starter, 3)
		assert.Equal(t, 3, starter.startedCount())
	})

	t.Run("winds down to the level asked", func(t *testing.T) {
		starter, fleet := newCountedFleet(t)

		require.NoError(t, fleet.HoldConnected(0, 6))
		awaitConnected(t, fleet, 6)

		require.NoError(t, fleet.HoldConnected(time.Second, 2))
		require.Eventually(t, func() bool { return starter.stoppedCount() == 4 },
			2*time.Second, 10*time.Millisecond, "the machines the run closed should have closed")
		awaitConnected(t, fleet, 2)
	})

	t.Run("a wind-down takes the machines asked for last", func(t *testing.T) {
		live := &liveIndexes{}
		fleet := NewQUICFleet(live.start)
		t.Cleanup(fleet.Stop)

		require.NoError(t, fleet.HoldConnected(0, 6))
		awaitConnected(t, fleet, 6)

		require.NoError(t, fleet.HoldConnected(time.Second, 2))
		awaitConnected(t, fleet, 2)
		require.Eventually(t, func() bool { return len(live.indexes()) == 2 },
			2*time.Second, 10*time.Millisecond, "the wind-down should have finished")

		assert.Equal(t, []int{0, 1}, live.indexes(),
			"the machines that arrived first should be the ones still connected")
	})

	t.Run("stopping ends every machine it started", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)

		require.NoError(t, fleet.HoldConnected(0, 4))
		fleet.Stop()

		assert.Equal(t, 0, fleet.Connected())
		assert.Equal(t, 4, starter.stoppedCount())
	})
}
