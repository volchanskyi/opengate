package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEachPhaseTakesALiveRoundTrip(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 10, probe: 42 * time.Millisecond}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.Positive(t, fleet.probes, "a phase that took no round trip has no latency to report")
	assert.InDelta(t, 42.0, results[0].LatencyP95Ms, 0.001)
}

func TestPhaseOutcomesComeFromWhatThePhaseSaw(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 6}
	fleet.outcomes = FleetOutcomes{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	failing := &failingFleet{meteredFleet: fleet}
	results, err := RunPhasesWatched(profile, failing, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.InDelta(t, 0.4, results[0].ErrorRate, 0.001, "four of every ten machines did not arrive")
	assert.EqualValues(t, 10, results[0].Faults, "a machine severed mid-hold is a fault")
	assert.EqualValues(t, 20, results[0].ExpectedRejections, "a declared refusal is the system working")
}

// failingFleet is a fleet whose machines partly fail on every hold.
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

func asErrors(reasons []string) []error {
	out := make([]error, len(reasons))
	for i, reason := range reasons {
		out[i] = errors.New(reason)
	}
	return out
}
