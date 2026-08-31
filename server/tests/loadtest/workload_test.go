package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A profile-driven run holds its machines connected on purpose, so the moment
// it reads what happened decides whether it saw a fleet or an empty room.
//
// A machine reports once, when its own life ends. Reading the fleet's results
// while every machine is still holding its level therefore reports nothing at
// all — no successes and no failures — which is indistinguishable from a fleet
// that never arrived, and is exactly the shape the volume family published for
// a night it ran perfectly well.

// holdingProfile is one short phase that leaves its machines connected. The
// drain phase is deliberately absent: what is being exercised is a run ending
// with its fleet still up.
func holdingProfile(agents int) *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "holding",
		Family:        FamilyNormal,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "steady", Duration: Duration{Duration: time.Minute}, ConnectedAgents: agents, OperatorArrivalsPerSecond: 2},
		},
		Safety: Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01},
	}
}

func TestAProfileRunReportsTheMachinesItWasStillHolding(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, phases, err := runProfile(holdingProfile(4), fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	// Every machine that held the level is in the results. Winding the fleet
	// down is what makes them report, so it has to happen before the reading.
	require.Len(t, results, 4,
		"a machine still connected when the profile ended is one the run measured")
	for _, result := range results {
		assert.NoError(t, result.err)
		assert.Positive(t, result.connectDur)
	}

	require.Len(t, phases, 1)
	assert.Equal(t, "steady", phases[0].Name)
}

func TestAProfileRunReportsTheMachinesThatNeverArrived(t *testing.T) {
	// Two arrive, the rest are refused at the dial.
	starter := &startCounter{failFrom: 2}
	fleet := NewQUICFleet(starter.start)
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, _, err := runProfile(holdingProfile(5), fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	require.Len(t, results, 5, "what arrived and what did not are both the run's account")
	var failed int
	for _, result := range results {
		if result.err != nil {
			failed++
		}
	}
	assert.Equal(t, 3, failed, "the gap between what was asked for and what arrived is the finding")
}

// A run the node stopped still says what it managed before it stopped, rather
// than discarding the machines it had already driven.
func TestAProfileRunStoppedByTheNodeStillReportsWhatItDrove(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	outOfRoom := func() NodeReading {
		return NodeReading{Measured: true, MemoryPercent: 99}
	}

	results, _, err := runProfile(holdingProfile(3), fleet, clock, outOfRoom)
	require.Error(t, err, "a node past its ceiling stops the run")
	assert.Empty(t, results, "nothing was driven, so nothing is reported")
}
