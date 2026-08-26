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
