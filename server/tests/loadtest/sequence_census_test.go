package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A phase says what the target was holding as well as what the harness believes
// it held. The two are counts of one population kept by the two ends, and only
// the second of them is a reading.

func TestAPhaseCarriesTheTargetsOwnAccountOfTheFleet(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// The target agrees with the harness at every level, which is what the pair
	// measured at two hundred and five hundred machines.
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: census})
	require.NoError(t, err)

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

// The reading is taken where the harness takes its own count, so the two
// describe the same instant. A census taken before the hold ended would compare
// the level the phase reached against the level it was climbing through.
func TestThePairOfCountsIsTakenAtTheSameLevel(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	var seen []int
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		seen = append(seen, fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held * 3, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: census})
	require.NoError(t, err)

	require.Len(t, seen, len(results), "one reading per phase, taken as the phase closes")
	for i, result := range results {
		assert.Equalf(t, result.AchievedConnectedAgents, seen[i],
			"phase %q read the target at the level it reports holding", result.Name)
	}
}

// A target that would not answer is accounted for rather than reported as a
// fleet of nought — the finding a capacity ladder climbs to reach.
func TestAPhaseAccountsForACensusItCouldNotTake(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	silent := TargetCensus{Read: func() (TargetHealth, bool) { return TargetHealth{}, false }}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: silent})
	require.NoError(t, err)

	for _, result := range results {
		assert.Nilf(t, result.TargetConnectedAgents, "phase %q reports no count it could not read", result.Name)
		assert.Equal(t, censusAbsentTargetSilent, result.TargetCensusAbsent)
	}
}

// A run pointed at no target carries neither, because there was no question to
// go unanswered.
func TestAPhaseWithNoTargetToReadCarriesNeither(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	for _, result := range results {
		assert.Nil(t, result.TargetConnectedAgents)
		assert.Nil(t, result.TargetGoroutines)
		assert.Empty(t, result.TargetCensusAbsent)
	}
}
