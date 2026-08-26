package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func threePhaseProfile() *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "test",
		Family:        FamilyNormal,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "ramp", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 250, OperatorArrivalsPerSecond: 2},
			{Name: "steady", Duration: Duration{Duration: 5 * time.Minute}, ConnectedAgents: 500, OperatorArrivalsPerSecond: 5, Sessions: 5},
			{Name: "drain", Duration: Duration{Duration: 30 * time.Second}, ConnectedAgents: 0},
		},
		Safety: Safety{MaxNodeCPUPercent: 85, MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01},
	}
}

func TestSequencerWalksEveryPhaseInOrder(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	require.Len(t, results, 3)
	assert.Equal(t, []string{"ramp", "steady", "drain"}, []string{results[0].Name, results[1].Name, results[2].Name})
}

func TestEachPhaseEndsAtTheLevelItDeclared(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	assert.Equal(t, 250, results[0].AchievedConnectedAgents)
	assert.Equal(t, 500, results[1].AchievedConnectedAgents)
	// A declared wind-down, so a connection the run closed is not counted as one
	// the server dropped.
	assert.Equal(t, 0, results[2].AchievedConnectedAgents)
}

func TestAPhaseRampsRatherThanStepping(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	_, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	// The first phase climbs to its level rather than arriving at it. A step
	// change is a different event — a site whose link came back — and the
	// profile says which one it wants. Only the first phase's instructions are
	// read here: elapsed time restarts with each phase, so a window on the clock
	// would pick up the next phase's opening steps as well.
	var rampTargets []int
	for i, step := range fleet.steps {
		if i >= rampSteps {
			break
		}
		assert.LessOrEqual(t, step.at, time.Minute, "a ramp instruction lands inside its own phase")
		rampTargets = append(rampTargets, step.target)
	}
	require.Greater(t, len(rampTargets), 2, "a ramp is more than one instruction")
	assert.Less(t, rampTargets[0], 250, "the fleet does not arrive all at once")
	for i := 1; i < len(rampTargets); i++ {
		assert.GreaterOrEqual(t, rampTargets[i], rampTargets[i-1], "a ramp never goes backwards")
	}
}
