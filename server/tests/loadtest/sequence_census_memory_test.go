package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A phase carries what the target held in memory at its census, beside the
// counts, so a night's summary can say how much of its memory ceiling the
// server used through the measured phase. A census nobody took carries none.
func TestAPhaseCarriesTheTargetsResidentMemoryAtItsCensus(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(fleet.Connected())
		return TargetHealth{Read: true, Goroutines: held * 3, ResidentBytes: 1_000_000 + held*1_000, AgentsConnected: &held}, true
	}}

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, PhaseReadings{Census: census})
	require.NoError(t, err)

	for _, result := range results {
		require.NotNilf(t, result.TargetResidentBytes, "phase %q carries the target's resident memory", result.Name)
		assert.InDelta(t, 1_000_000+float64(result.AchievedConnectedAgents)*1_000, *result.TargetResidentBytes, 0.5)
	}

	silent := TargetCensus{Read: func() (TargetHealth, bool) { return TargetHealth{}, false }}
	results, err = RunPhasesWatched(profile, &recordingFleet{}, clock, alwaysRoomToRun, PhaseReadings{Census: silent})
	require.NoError(t, err)
	for _, result := range results {
		assert.Nilf(t, result.TargetResidentBytes, "phase %q took no census, so it carries no reading of nought", result.Name)
	}
}
