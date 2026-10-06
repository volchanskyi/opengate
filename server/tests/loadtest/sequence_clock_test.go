package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPhaseIsAsLongAsItActuallyTook(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{probeCost: 4 * time.Second}
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet.clock = clock

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	// Ten round trips of four seconds each, on top of the minute the first phase declares.
	assert.Equal(t, start.Add(time.Minute+40*time.Second), results[0].FinishedAt,
		"a phase finished when it finished")
	assert.Equal(t, results[0].FinishedAt, results[1].StartedAt,
		"the next phase starts where this one ended, not where it said it would")
}

func TestAttainmentComparesCountsOverTheSameClock(t *testing.T) {
	profile := threePhaseProfile()
	// The fleet delivers four of every five machines asked of it.
	fleet := &recordingFleet{arriveNumerator: 4, arriveDenominator: 5, probeCost: 4 * time.Second}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	fleet.clock = clock

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.InDelta(t, 0.8, results[1].AchievedFraction(), 0.01,
		"four machines in five is four fifths of the offer, whatever the phase's wall clock did")
}
