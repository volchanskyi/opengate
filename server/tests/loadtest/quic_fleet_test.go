package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fleet is asked to hold a level, and how it gets there matters: a machine
// already connected stays connected, and one the run winds down closes
// deliberately rather than counting as one the server dropped.
func TestFleetHoldsTheLevelItIsAskedFor(t *testing.T) {
	t.Run("climbs to the level", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 5))

		// Each machine runs on its own, so the dialling catches up a moment
		// later and the fleet is five once all five have arrived.
		require.Eventually(t, func() bool { return starter.startedCount() == 5 },
			2*time.Second, 10*time.Millisecond)
		awaitConnected(t, fleet, 5)
	})

	t.Run("climbs again without restarting what is already up", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 3))
		require.NoError(t, fleet.HoldConnected(time.Second, 8))

		// Rebuilding the fleet at each step would measure the accept path over
		// and over and never measure a fleet that is simply there: eight were
		// asked for and eight dialled, not three and then eight more.
		require.Eventually(t, func() bool { return starter.startedCount() == 8 },
			2*time.Second, 10*time.Millisecond)
		awaitConnected(t, fleet, 8)
	})

	t.Run("holding the level it already holds starts nothing", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 3))
		require.NoError(t, fleet.HoldConnected(time.Second, 3))

		require.Eventually(t, func() bool { return starter.startedCount() == 3 },
			2*time.Second, 10*time.Millisecond)
		assert.Equal(t, 3, starter.startedCount())
	})

	t.Run("winds down to the level asked", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 6))
		awaitConnected(t, fleet, 6)

		require.NoError(t, fleet.HoldConnected(time.Second, 2))
		require.Eventually(t, func() bool { return starter.stoppedCount() == 4 },
			2*time.Second, 10*time.Millisecond, "the machines the run closed should have closed")
		// A machine the run closed has left the connected once its own life has
		// finished, which is after the dialling side recorded the close — so
		// this is its own wait rather than a reading taken off that one.
		awaitConnected(t, fleet, 2)
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
