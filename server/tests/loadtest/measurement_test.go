package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// meteredFleet is a fleet whose readings the test sets, held below the rate it was asked for.
type meteredFleet struct {
	connected       int
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

func TestAchievedArrivalRateIsMeasuredRatherThanRestated(t *testing.T) {
	// Two arrivals per instruction across ten instructions deliver twenty of the hundred asked for.
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 2}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
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

func TestAPhaseBelowItsOfferedRateInvalidatesTheRun(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 2}
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
	assert.Contains(t, errors.Join(asErrors(verdict.Reasons)...).Error(), "of the offered arrival rate")
}

func TestTheOperatorRateIsCarriedWithoutBeingClaimedAsAchieved(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	fleet := &meteredFleet{arrivalsPerStep: 10}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.InDelta(t, 5.0, results[0].OfferedOperatorArrivalsPerSecond, 0.001,
		"what the profile asked for travels with the run")
	assert.Nil(t, results[0].AchievedOperatorArrivalsPerSecond,
		"this process drives machines, so the technician rate it never offered is absent rather than assumed")
}

func TestTheSessionCountIsCarriedWithoutBeingClaimedAsRun(t *testing.T) {
	profile := onePhaseProfile(100, 10*time.Second)
	profile.Phases[0].Sessions = 5
	fleet := &meteredFleet{arrivalsPerStep: 10}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.Equal(t, 5, results[0].OfferedSessions, "what the profile asked for travels with the run")
	assert.Nil(t, results[0].AchievedSessions,
		"nothing here opens a session, so the count it never ran is absent rather than assumed")
}
