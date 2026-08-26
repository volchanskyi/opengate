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
		assert.Equal(t, 5, fleet.Connected())

		// Each machine runs on its own, so the fleet's own count is true
		// immediately and the dialling catches up a moment later.
		require.Eventually(t, func() bool { return starter.startedCount() == 5 },
			2*time.Second, 10*time.Millisecond)
	})

	t.Run("climbs again without restarting what is already up", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 3))
		require.NoError(t, fleet.HoldConnected(time.Second, 8))
		assert.Equal(t, 8, fleet.Connected())

		// Rebuilding the fleet at each step would measure the accept path over
		// and over and never measure a fleet that is simply there.
		require.Eventually(t, func() bool { return starter.startedCount() == 8 },
			2*time.Second, 10*time.Millisecond)
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
		require.NoError(t, fleet.HoldConnected(time.Second, 2))
		assert.Equal(t, 2, fleet.Connected())

		require.Eventually(t, func() bool { return starter.stoppedCount() == 4 },
			2*time.Second, 10*time.Millisecond, "the machines the run closed should have closed")
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
