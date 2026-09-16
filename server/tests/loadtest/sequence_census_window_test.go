package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two counts of the fleet are comparable only while they describe one
// instant, so the run brackets the target's answer with its own: it counts, it
// asks, and it counts again. What the two may then differ by is what the fleet
// itself recorded leaving in between.

// The target's answer is bracketed by the run's own counts, and what leaves in
// between is recorded. Without both terms the only way to allow for a fleet that
// changed under the question is a percentage — and a percentage of the fleet
// cannot express a quantity that has nothing to do with fleet size.
func TestAPhaseRecordsWhatItWasHoldingEitherSideOfTheQuestion(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// A machine leaves while the question is in flight, which is the soak's
	// ordinary condition: it replaces every machine it loses.
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		fleet.connected--
		fleet.outcomes.Departed++
		held := float64(fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: census})
	require.NoError(t, err)

	require.Len(t, results, 3)
	for _, want := range []struct {
		phase  int
		before int
	}{{0, 250}, {1, 500}, {2, 0}} {
		result := results[want.phase]
		assert.Equalf(t, want.before, result.ConnectedAgentsBeforeCensus,
			"phase %q says what it was holding before it asked", result.Name)
		assert.Equalf(t, int64(1), result.DeparturesDuringCensus,
			"phase %q says what left while it was asking", result.Name)
	}
}
