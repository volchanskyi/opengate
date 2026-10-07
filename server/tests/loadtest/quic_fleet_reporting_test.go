package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetReportsWhatArrivedAndWhatDidNot(t *testing.T) {
	t.Run("machines that never arrived leave the level", func(t *testing.T) {
		starter := &startCounter{failFrom: 2}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 5))

		awaitFailures(t, fleet, 3)
		awaitConnected(t, fleet, 2)
	})

	t.Run("a machine that never arrived is not replaced by another dial", func(t *testing.T) {
		starter := &startCounter{failFrom: 2}
		fleet := NewQUICFleet(starter.start)

		// Awaiting the refusals at each step keeps the replacement race deterministic.
		for _, elapsed := range []time.Duration{0, time.Second, 2 * time.Second} {
			require.NoError(t, fleet.HoldConnected(elapsed, 5))
			awaitFailures(t, fleet, 3)
			awaitConnected(t, fleet, 2)
		}

		fleet.Stop()
		assert.Equal(t, 5, starter.startedCount(), "the level was asked for once")
		assert.Len(t, fleet.Results(), 5, "one account per machine the profile asked for")
	})

	t.Run("winding down past machines that never arrived leaves the rest alone", func(t *testing.T) {
		starter := &startCounter{failFrom: 3}
		fleet := NewQUICFleet(starter.start)

		require.NoError(t, fleet.HoldConnected(0, 6))
		awaitFailures(t, fleet, 3)
		awaitConnected(t, fleet, 3)

		require.NoError(t, fleet.HoldConnected(time.Second, 3))
		assert.Equal(t, 3, fleet.Connected(), "the machines that arrived are the ones still holding")

		fleet.Stop()
		assert.Len(t, fleet.Results(), 6, "one account per machine the profile asked for")
		assert.Len(t, fleet.Failures(), 3)
	})

	t.Run("every machine's timing is kept", func(t *testing.T) {
		starter := &startCounter{}
		fleet := NewQUICFleet(starter.start)

		require.NoError(t, fleet.HoldConnected(0, 3))
		fleet.Stop()

		results := fleet.Results()
		assert.Len(t, results, 3)
		for _, result := range results {
			assert.NoError(t, result.err)
			assert.Positive(t, result.connectDur)
		}
	})

	t.Run("no round trip is taken by a fleet with no prober", func(t *testing.T) {
		fleet := NewQUICFleet(func(ctx context.Context, _ int, presence fleetPresence) agentResult {
			<-ctx.Done()
			return agentResult{}
		})
		defer fleet.Stop()

		assert.Zero(t, fleet.ProbeLatency())
	})
}
