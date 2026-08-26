package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a run reports about the phases it walked, and what it refuses to walk.

func TestOfferedLoadIsRecordedBesideAchievedLoad(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	// Without both, a generator that could not keep up and a server that was
	// slow are the same reading.
	assert.Equal(t, 500, results[1].OfferedConnectedAgents)
	assert.Equal(t, 500, results[1].AchievedConnectedAgents)
	assert.InDelta(t, 5.0, results[1].OfferedArrivalsPerSecond, 0.001)
}

func TestPhaseBoundariesAreRecorded(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)

	assert.Equal(t, start, results[0].StartedAt)
	assert.Equal(t, start.Add(time.Minute), results[0].FinishedAt)
	assert.Equal(t, results[0].FinishedAt, results[1].StartedAt)
	assert.Equal(t, start.Add(profile.TotalDuration()), results[2].FinishedAt)
}

func TestAFleetThatStopsAnsweringEndsTheRun(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{failAfter: 3}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	_, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped answering")
}

func TestASingleInstantPhaseIsStillOneInstruction(t *testing.T) {
	profile := threePhaseProfile()
	profile.Phases = []Phase{
		{Name: "spike", Duration: Duration{Duration: time.Second}, ConnectedAgents: 400},
	}
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 400, results[0].AchievedConnectedAgents)
}

func TestRunPhasesRefusesAProfileWithNoPhases(t *testing.T) {
	profile := threePhaseProfile()
	profile.Phases = nil

	_, err := RunPhasesWatched(profile, &recordingFleet{}, &testClock{now: time.Now()}, alwaysRoomToRun)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no phases")
}

func TestRunPhasesRefusesAMissingProfile(t *testing.T) {
	_, err := RunPhasesWatched(nil, &recordingFleet{}, &testClock{now: time.Now()}, alwaysRoomToRun)
	require.Error(t, err)
}
