package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two counts of the fleet are comparable only while they describe one
// instant, so the run brackets the target's answer with its own: it counts, it
// asks, and it counts again. What the two may then differ by is what the fleet
// itself recorded leaving in between.

// The target's answer is bracketed by the run's own counts, and what leaves in
// between is recorded. Without both terms the only way to allow for a fleet that
// changed under the question is a percentage — and a percentage of the fleet
// cannot express a quantity that has nothing to do with fleet size.
func TestAPhaseRecordsWhatItWasHoldingEitherSideOfTheQuestion(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// One machine leaves while the question is in flight, which is the soak's
	// ordinary condition: it replaces every machine it loses. The target then
	// accounts for the rest, so the phase asks once and the departure is the
	// whole of what the two counts differ by.
	asked := 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		asked++
		fleet.connected--
		fleet.outcomes.Departed++
		held := float64(fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: census})
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

// A target still admitting machines it has already accepted is waited out, and
// the phase says how long for. The wait is the third delay between the two
// counts — the first two were the run counting dials and the target copying its
// count in on a timer — and it is the only one neither end can remove: a
// machine has handshaken and asked to register before the target has put it in
// the map it counts.
func TestAPhaseSaysHowLongItHeldStillForTheTarget(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	// The target is two readings behind the fleet each time it is asked, which
	// is a target working through the machines it has accepted.
	behind := 2
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		if behind > 0 {
			behind--
			held -= 40
		}
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: census})
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

// The wait belongs to neither phase, so neither pays for it. A phase's
// busy-ness is the target's own processor counter over the phase's own clock,
// and charging a wait to one side of that division reports a figure nobody
// measured — on the leg where the wait is longest, which is the leg already
// working hardest.
func TestTheBusyReadingDoesNotCountTheTimeSpentWaitingForTheTarget(t *testing.T) {
	profile := threePhaseProfile()

	busyOver := func(census TargetCensus) *float64 {
		fleet := &recordingFleet{}
		clock := &testClock{now: time.Unix(1_800_000_000, 0)}
		// A target spending half of every second of wall clock, so the share it
		// used is whatever window the reading is divided by.
		origin := clock.now
		busy := TargetBusy{
			Allowance:      1,
			ReadCPUSeconds: func() (float64, bool) { return clock.Now().Sub(origin).Seconds() / 2, true },
		}
		results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun,
			TargetReading{Busy: busy, Census: census})
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

// fleetCountingCensus is a target whose answer is whatever the case says, given
// the level the phase is holding.
func fleetCountingCensus(answer func(held int) float64) TargetCensus {
	return TargetCensus{Read: func() (TargetHealth, bool) {
		held := answer(250)
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}
}

// The night of 2026-09-16, walked. A steady phase climbing to eight thousand
// machines against a target with one processor: the run held 7,946 and the
// target's first answer was 7,883, which the rule reads — correctly, on the
// numbers it is given — as a level the system was not carrying. Waiting the
// target out is what makes those the same number, and the run is a measurement
// again.
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

	// At every phase close the target is behind by the machines it has accepted
	// and not yet admitted, and it works through them once the run stops
	// offering arrivals.
	level, owed := -1, 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		if fleet.Connected() != level {
			level, owed = fleet.Connected(), 63
		}
		owed = max(owed-30, 0)
		held := float64(level - owed)
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, TargetReading{Census: census})
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
