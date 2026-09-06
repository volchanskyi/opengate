package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What arrived and what did not are both findings, and the gap between them is
// the one a run exists to report.
func TestFleetReportsWhatArrivedAndWhatDidNot(t *testing.T) {
	t.Run("machines that never arrived leave the level", func(t *testing.T) {
		starter := &startCounter{failFrom: 2}
		fleet := NewQUICFleet(starter.start)
		defer fleet.Stop()

		require.NoError(t, fleet.HoldConnected(0, 5))

		// Asked for five, two arrived. The count reports what is actually there
		// rather than what was requested.
		require.Eventually(t, func() bool { return fleet.Connected() == 2 },
			2*time.Second, 10*time.Millisecond)
		assert.Len(t, fleet.Failures(), 3)
	})

	t.Run("a machine that never arrived is not replaced by another dial", func(t *testing.T) {
		starter := &startCounter{failFrom: 2}
		fleet := NewQUICFleet(starter.start)

		// Three steps of a ramp that has already reached its level. Each one is
		// the sequencer restating the level, not a new instruction.
		//
		// The wait between them is the phase's own step interval, and it is what
		// makes this deterministic: a machine is only replaceable once its
		// refusal has been recorded, so a test that restates the level in a
		// tight loop races the refusals and usually finds the level still whole.
		// That race is the defect, not the harness — on a runner the steps are
		// milliseconds apart and the refusals land between them.
		for _, elapsed := range []time.Duration{0, time.Second, 2 * time.Second} {
			require.NoError(t, fleet.HoldConnected(elapsed, 5))
			require.Eventually(t, func() bool { return fleet.Connected() == 2 },
				2*time.Second, 10*time.Millisecond,
				"three machines were refused at the dial, so two are connected")
		}

		// Reading the level off what is connected makes each of those steps
		// dial a replacement for every machine that never arrived. The run then
		// reports the same refusal once per ramp step under a new machine every
		// time, so how many machines never arrived becomes a property of how
		// many steps the phase happened to have rather than of what the profile
		// asked for — and the count is whatever the scheduler decided, because
		// a machine is replaced only once its own refusal has been recorded.
		//
		// Winding down is what makes every machine report, so it comes before
		// the reading.
		fleet.Stop()
		assert.Equal(t, 5, starter.startedCount(), "the level was asked for once")
		assert.Len(t, fleet.Results(), 5, "one account per machine the profile asked for")
	})

	t.Run("winding down past machines that never arrived leaves the rest alone", func(t *testing.T) {
		starter := &startCounter{failFrom: 3}
		fleet := NewQUICFleet(starter.start)

		require.NoError(t, fleet.HoldConnected(0, 6))
		require.Eventually(t, func() bool { return fleet.Connected() == 3 },
			2*time.Second, 10*time.Millisecond)

		// Three of the six never arrived, so winding down to three is winding
		// down the three that never arrived. Keeping them in the level is what
		// makes that subtraction land on them: without it the wind-down closes
		// three machines that are carrying the load and the phase holds nothing.
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

	t.Run("no latency before anything connects", func(t *testing.T) {
		fleet := NewQUICFleet(func(ctx context.Context, _ int) agentResult {
			<-ctx.Done()
			return agentResult{}
		})
		defer fleet.Stop()

		assert.Zero(t, fleet.SampleLatency())
	})
}
