package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A phase is as long as the clock says, and what it compares is counts taken
// over that same clock.

// A phase's boundaries were its declaration: it started when the clock said so
// and finished a declared duration later, whatever the walk had actually spent.
// A sweep leg whose profile declared three and a half minutes ran for fifteen
// and forty-two, and every phase in its bundle said otherwise — so the rate
// each phase divided its arrivals by was a denominator no clock had produced.
func TestAPhaseIsAsLongAsItActuallyTook(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{probeCost: 4 * time.Second}
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet.clock = clock

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	// Ten round trips per phase, each costing four seconds of the phase it was
	// taken in, on top of the minute the first phase declares.
	assert.Equal(t, start.Add(time.Minute+40*time.Second), results[0].FinishedAt,
		"a phase finished when it finished")
	assert.Equal(t, results[0].FinishedAt, results[1].StartedAt,
		"the next phase starts where this one ended, not where it said it would")
}

// The offered and achieved rates are both counts over the phase's own clock, so
// what they compare is the machines asked for against the machines that turned
// up. Dividing the offer by a declaration and the arrivals by the same
// declaration hides a phase that ran long; dividing both by what the phase took
// states attainment and nothing else.
func TestAttainmentComparesCountsOverTheSameClock(t *testing.T) {
	profile := threePhaseProfile()
	// A fleet that delivers four of every five machines asked of it.
	fleet := &recordingFleet{arriveNumerator: 4, arriveDenominator: 5, probeCost: 4 * time.Second}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	fleet.clock = clock

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.InDelta(t, 0.8, results[1].AchievedFraction(), 0.01,
		"four machines in five is four fifths of the offer, whatever the phase's wall clock did")
}
