package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPhaseRecordsWhatItWasHoldingEitherSideOfTheQuestion(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// One machine leaves while the question is in flight, so the departure is the whole difference.
	asked := 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		asked++
		fleet.connected--
		fleet.outcomes.Departed++
		held := float64(fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, PhaseReadings{Census: census})
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
		assert.Zerof(t, result.TargetCensusWaitedMs,
			"phase %q was not held open by the machine that left", result.Name)
	}
	assert.Equal(t, len(results), asked,
		"a target that accounts for the fleet the run still has is asked once per phase")
}

func TestAPhaseSaysHowLongItHeldStillForTheTarget(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// The target is two readings behind the fleet, as if still admitting accepted machines.
	behind := 2
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		if behind > 0 {
			behind--
			held -= 40
		}
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, PhaseReadings{Census: census})
	require.NoError(t, err)

	require.Len(t, results, 3)
	assert.Equal(t, millis(2*censusSettleInterval), results[0].TargetCensusWaitedMs,
		"the ramp held still while the target caught up")
	require.NotNil(t, results[0].TargetConnectedAgents)
	assert.Equal(t, 250, *results[0].TargetConnectedAgents,
		"and the count it recorded is the one the target settled on")
	assert.Zero(t, results[1].TargetCensusWaitedMs,
		"a target already accounting for the fleet is not waited on")
}

func TestTheBusyReadingDoesNotCountTheTimeSpentWaitingForTheTarget(t *testing.T) {
	profile := threePhaseProfile()

	busyOver := func(census TargetCensus) *float64 {
		fleet := &recordingFleet{}
		clock := &testClock{now: time.Unix(1_800_000_000, 0)}
		// The target spends half of every second of wall clock.
		origin := clock.now
		busy := TargetBusy{
			Allowance:      1,
			ReadCPUSeconds: func() (float64, bool) { return clock.Now().Sub(origin).Seconds() / 2, true },
		}
		results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun,
			PhaseReadings{Busy: busy, Census: census})
		require.NoError(t, err)
		return results[0].TargetBusyPercent
	}

	answered := func(int) float64 { return 250 }
	prompt := busyOver(fleetCountingCensus(answered))
	behind := 4
	waited := busyOver(fleetCountingCensus(func(held int) float64 {
		if behind > 0 {
			behind--
			return float64(held - 40)
		}
		return float64(held)
	}))

	require.NotNil(t, prompt)
	require.NotNil(t, waited)
	assert.InDelta(t, 50.0, *prompt, 0.001, "half a processor of a whole one is half of it")
	assert.InDelta(t, *prompt, *waited, 0.001,
		"and the same phase reads the same whether or not the target had to be waited on")
}

func fleetCountingCensus(answer func(held int) float64) TargetCensus {
	return TargetCensus{Read: func() (TargetHealth, bool) {
		held := answer(250)
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}
}

func TestAPhaseTheTargetCaughtUpWithIsARunThatMeasuredTheSystem(t *testing.T) {
	profile := &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "volume-8000",
		Family:        FamilyVolume,
		Environment:   EnvRunner,
		Fixture:       FixtureLopsided,
		Phases: []Phase{
			{Name: "ramp", Duration: Duration{Duration: 2 * time.Minute}, ConnectedAgents: 1600, OperatorArrivalsPerSecond: 2},
			{Name: "steady", Duration: Duration{Duration: 3 * time.Minute}, ConnectedAgents: 7946, OperatorArrivalsPerSecond: 5, Sessions: 5},
		},
		Safety: Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01},
	}
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// At each phase close the target owes the machines it accepted and not yet admitted,
	// and works through them once the run stops offering arrivals.
	level, owed := -1, 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		if fleet.Connected() != level {
			level, owed = fleet.Connected(), 63
		}
		owed = max(owed-30, 0)
		held := float64(level - owed)
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, PhaseReadings{Census: census})
	require.NoError(t, err)

	verdict := classify(func(in *RunInputs) {
		in.Profile = profile
		in.Phases = results
	})
	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)

	steady := results[len(results)-1]
	require.NotNil(t, steady.TargetConnectedAgents)
	assert.Equal(t, 7946, *steady.TargetConnectedAgents)
	assert.Positive(t, steady.TargetCensusWaitedMs, "and the phase says the target had to be waited on")
}
