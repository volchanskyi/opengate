package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runThreePhasesWatched(t *testing.T, fleet *recordingFleet, readings PhaseReadings) []PhaseResult {
	t.Helper()
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	results, err := RunPhasesWatched(threePhaseProfile(), fleet, clock, alwaysRoomToRun, readings)
	require.NoError(t, err)
	return results
}

func TestAPhaseCarriesTheTargetsOwnAccountOfTheFleet(t *testing.T) {
	fleet := &recordingFleet{}
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results := runThreePhasesWatched(t, fleet, PhaseReadings{Census: census})

	require.Len(t, results, 3)
	for _, want := range []struct {
		phase int
		held  int
	}{{0, 250}, {1, 500}, {2, 0}} {
		result := results[want.phase]
		require.NotNilf(t, result.TargetConnectedAgents, "phase %q carries the target's count", result.Name)
		assert.Equal(t, want.held, *result.TargetConnectedAgents)
		require.NotNil(t, result.TargetGoroutines)
		assert.Equal(t, float64(want.held)*3+29, *result.TargetGoroutines)
		assert.Empty(t, result.TargetCensusAbsent)
	}
}

func TestThePairOfCountsIsTakenAtTheSameLevel(t *testing.T) {
	fleet := &recordingFleet{}
	var seen []int
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		seen = append(seen, fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held * 3, AgentsConnected: &held}, true
	}}

	results := runThreePhasesWatched(t, fleet, PhaseReadings{Census: census})

	require.Len(t, seen, len(results), "one reading per phase, taken as the phase closes")
	for i, result := range results {
		assert.Equalf(t, result.AchievedConnectedAgents, seen[i],
			"phase %q read the target at the level it reports holding", result.Name)
	}
}

func TestAPhaseAccountsForACensusItCouldNotTake(t *testing.T) {
	silent := TargetCensus{Read: func() (TargetHealth, bool) { return TargetHealth{}, false }}

	results := runThreePhasesWatched(t, &recordingFleet{}, PhaseReadings{Census: silent})

	for _, result := range results {
		assert.Nilf(t, result.TargetConnectedAgents, "phase %q reports no count it could not read", result.Name)
		assert.Equal(t, censusAbsentTargetSilent, result.TargetCensusAbsent)
	}
}

func TestAPhaseWithNoTargetToReadCarriesNeither(t *testing.T) {
	results := runThreePhasesWatched(t, &recordingFleet{}, unreadTarget)

	for _, result := range results {
		assert.Nil(t, result.TargetConnectedAgents)
		assert.Nil(t, result.TargetGoroutines)
		assert.Empty(t, result.TargetCensusAbsent)
	}
}
