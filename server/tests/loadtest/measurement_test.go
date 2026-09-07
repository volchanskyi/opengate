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

// The same for concurrent sessions. A profile declaring five of them was
// running zero, because opening a session is the technician's side of the wire
// and this process drives the machine's. What it asked for travels so a reader
// can see the gap; what nothing offered stays absent.
func TestTheSessionCountIsCarriedWithoutBeingClaimedAsRun(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	profile.Phases[0].Sessions = 5
	fleet := &meteredFleet{arrivalsPerStep: 10}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	assert.Equal(t, 5, results[0].OfferedSessions, "what the profile asked for travels with the run")
	assert.Nil(t, results[0].AchievedSessions,
		"nothing here opens a session, so the count it never ran is absent rather than assumed")
}
