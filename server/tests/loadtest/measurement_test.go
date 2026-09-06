package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every number a bundle carries is either a reading or absent.
//
// The defect class these cover is one shape wearing several costumes: a field
// declared so a later reader could interpret the run, assigned from a literal
// or from the thing it was supposed to be compared against, and validated by a
// rule that therefore cannot fire. A run of 500 machines reported a target with
// one processor and one byte of memory, a generator with 100% headroom, an
// attainment of exactly 1.0 and a fleet count that counted machines which had
// already left — and every one of those passed every gate.
//
// So each case below breaks one measurement and requires the run to notice.

// meteredFleet is a fleet whose readings the test decides. It exists to hold a
// fleet deliberately below the rate it was asked for, which is the one case the
// attainment rule was written for and the one case a fleet that restates its
// instructions can never produce.
type meteredFleet struct {
	connected int
	// arrivalsPerStep is how many machines actually turn up at each instruction,
	// which the test sets below what the phase asks for.
	arrivalsPerStep int
	outcomes        FleetOutcomes
	probe           time.Duration
	probes          int
}

func (f *meteredFleet) HoldConnected(_ time.Duration, target int) error {
	f.connected = target
	f.outcomes.Arrived += int64(f.arrivalsPerStep)
	return nil
}

func (f *meteredFleet) Connected() int { return f.connected }

func (f *meteredFleet) ProbeLatency() time.Duration {
	f.probes++
	return f.probe
}

func (f *meteredFleet) Outcomes() FleetOutcomes { return f.outcomes }

func onePhaseProfile(connected int, duration time.Duration) *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "test",
		Family:        FamilyNormal,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{{
			Name:                      "steady",
			Duration:                  Duration{Duration: duration},
			ConnectedAgents:           connected,
			OperatorArrivalsPerSecond: 5,
		}},
		Safety: Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.5},
	}
}

// D6. The attainment ratio was assigned from the offered rate, so it was
// exactly 1.0 on every run ever recorded and the rule that invalidates a run
// below 80% could not fire.
func TestAchievedArrivalRateIsMeasuredRatherThanRestated(t *testing.T) {
	// A hundred machines asked for over ten seconds is ten a second; two turn up
	// per instruction against ten instructions, so twenty arrive and the fleet
	// delivered a fifth of what it was told to.
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 2}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.InDelta(t, 10.0, results[0].OfferedAgentArrivalsPerSecond, 0.001,
		"the offered rate is the climb the phase declared")
	assert.InDelta(t, 2.0, results[0].AchievedAgentArrivalsPerSecond, 0.001,
		"the achieved rate is what the fleet actually delivered")
	assert.NotEqual(t, results[0].OfferedAgentArrivalsPerSecond, results[0].AchievedAgentArrivalsPerSecond,
		"a restated rate is not a measurement")
	assert.Less(t, results[0].AchievedFraction(), minAchievedFraction,
		"a fleet at a fifth of its offered rate is below the attainment floor")
}

// The rule the measurement above exists to feed. It had never fired.
func TestAPhaseBelowItsOfferedRateInvalidatesTheRun(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 2}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	verdict := Classify(RunInputs{
		Profile:           profile,
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{Measured: true, CPUHeadroomPercent: 90, MemoryUsedPercent: 10},
		Phases:            results,
	})

	assert.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, errors.Join(asErrors(verdict.Reasons)...).Error(), "of the offered arrival rate")
}

// The profile's technician numbers describe load this process does not drive.
// Carrying them beside an absent achieved figure is honest; restating them as
// achieved is what D6 was.
func TestTheOperatorRateIsCarriedWithoutBeingClaimedAsAchieved(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 10}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	assert.InDelta(t, 5.0, results[0].OfferedOperatorArrivalsPerSecond, 0.001,
		"what the profile asked for travels with the run")
	assert.Nil(t, results[0].AchievedOperatorArrivalsPerSecond,
		"this process drives machines, so the technician rate it never offered is absent rather than assumed")
}

// D10. The phase's latency was the last finished machine's connect time, and in
// a profiled run no machine finishes during the walk — so every phase of every
// bundle carried a null.
func TestEachPhaseTakesALiveRoundTrip(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 10, probe: 42 * time.Millisecond}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
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
	results, err := RunPhasesWatched(profile, failing, clock, alwaysRoomToRun)
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

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
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
