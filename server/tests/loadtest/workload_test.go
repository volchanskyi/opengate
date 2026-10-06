package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdingProfile is one phase with no drain and no length, since the walk uses a test clock
// while the fleet dials on the real one, so every machine arrives at once.
func holdingProfile(agents int) *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "holding",
		Family:        FamilyNormal,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "steady", ConnectedAgents: agents, OperatorArrivalsPerSecond: 2},
		},
		Safety: Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01},
	}
}

func TestAProfileRunReportsTheMachinesItWasStillHolding(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, phases, err := runProfile(holdingProfile(4), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

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
	// Two machines arrive and the rest are refused at the dial.
	starter := &startCounter{failFrom: 2}
	fleet := NewQUICFleet(starter.start)
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, _, err := runProfile(holdingProfile(5), fleet, clock, alwaysRoomToRun, unreadTarget)
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

func TestAProfileRunStoppedByTheNodeStillReportsWhatItDrove(t *testing.T) {
	starter := &startCounter{}
	fleet := NewQUICFleet(starter.start)
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	outOfRoom := func() NodeReading {
		return NodeReading{Measured: true, MemoryPercent: 99}
	}

	results, _, err := runProfile(holdingProfile(3), fleet, clock, outOfRoom, unreadTarget)
	require.Error(t, err, "a node past its ceiling stops the run")
	assert.Empty(t, results, "nothing was driven, so nothing is reported")
}
