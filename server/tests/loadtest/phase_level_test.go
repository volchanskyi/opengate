package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPhaseClimbsFromTheLevelTheFleetHoldsWhileItsMachinesWait(t *testing.T) {
	profile := threePhaseProfile()
	profile.Phases = []Phase{
		{Name: "return", Duration: Duration{Duration: 30 * time.Second}, ConnectedAgents: 5000},
		{Name: "drain", Duration: Duration{Duration: 24 * time.Minute}, ConnectedAgents: 5000},
	}
	fleet := &recordingFleet{signedInAtMost: 395}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	require.Len(t, fleet.steps, 2*rampSteps)
	for _, step := range fleet.steps[rampSteps:] {
		assert.Equal(t, 5000, step.target, "the drain winds down machines still waiting to sign in")
	}
	assert.Equal(t, 5000, fleet.asked)
	assert.Equal(t, 395, results[0].AchievedConnectedAgents)
	assert.Zero(t, results[1].OfferedAgentArrivalsPerSecond)
}
