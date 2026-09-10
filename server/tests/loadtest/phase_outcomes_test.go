package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a phase reports about itself, rather than about the instructions it was
// given.
//
// A phase's latency was the last finished machine's connect time, and in a
// profiled run no machine finishes while the walk is running — so every phase of
// every bundle carried nothing, under a field that is omitted when empty and
// complained about by nothing. Its error rate, its faults and the refusals the
// server made on purpose were never assigned at all, which left the profile's
// own error ceiling dead for every profiled run.

// D10. The phase's latency was the last finished machine's connect time, and in
// a profiled run no machine finishes during the walk — so every phase of every
// bundle carried a null.
func TestEachPhaseTakesALiveRoundTrip(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 10, probe: 42 * time.Millisecond}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.Positive(t, fleet.probes, "a phase that took no round trip has no latency to report")
	assert.InDelta(t, 42.0, results[0].LatencyP95Ms, 0.001)
}

// D16. Three fields were declared, never assigned, and therefore zero in every
// phase of every profiled run — which left the error-rate ceiling dead there.
func TestPhaseOutcomesComeFromWhatThePhaseSaw(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 6}
	// Four of every ten instructed machines fail, one of them by losing a
	// connection it was holding, and two are refused on purpose.
	fleet.outcomes = FleetOutcomes{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// The fleet's own tallies move as the phase runs, so they are advanced by
	// the same instruction count the walk uses.
	failing := &failingFleet{meteredFleet: fleet}
	results, err := RunPhasesWatched(profile, failing, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.InDelta(t, 0.4, results[0].ErrorRate, 0.001, "four of every ten machines did not arrive")
	assert.EqualValues(t, 10, results[0].Faults, "a machine severed mid-hold is a fault")
	assert.EqualValues(t, 20, results[0].ExpectedRejections, "a declared refusal is the system working")
}

// failingFleet is a fleet whose machines partly fail, so the phase has outcomes
// other than success to report.
type failingFleet struct {
	*meteredFleet
}

func (f *failingFleet) HoldConnected(elapsed time.Duration, target int) error {
	if err := f.meteredFleet.HoldConnected(elapsed, target); err != nil {
		return err
	}
	f.outcomes.Failed += 4
	f.outcomes.Severed++
	f.outcomes.Rejected += 2
	return nil
}

// A phase whose error rate is past the profile's own ceiling stops being a
// measurement of the system and becomes one of the error path.
func TestAPhaseErrorRatePastTheCeilingInvalidatesTheRun(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	profile.Safety.MaxErrorRate = 0.1
	fleet := &failingFleet{meteredFleet: &meteredFleet{arrivalsPerStep: 6}}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	verdict := Classify(RunInputs{
		Profile:           profile,
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{Measured: true, CPUHeadroomPercent: 90, MemoryUsedPercent: 10},
		Phases:            results,
	})

	assert.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, errors.Join(asErrors(verdict.Reasons)...).Error(), "describe the error path")
}

// asErrors turns a verdict's reasons into something joinable, so a case can
// assert on all of them at once rather than on whichever came first.
func asErrors(reasons []string) []error {
	out := make([]error, len(reasons))
	for i, reason := range reasons {
		out[i] = errors.New(reason)
	}
	return out
}
