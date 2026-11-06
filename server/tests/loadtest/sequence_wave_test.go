package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spikeShapedProfile() *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "test",
		Family:        FamilySpike,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "baseline", Duration: Duration{Duration: 2 * time.Minute}, ConnectedAgents: 500},
			{Name: "spike", Duration: Duration{Duration: 30 * time.Second}, ConnectedAgents: 2000, Measured: true},
			{Name: "recovery", Duration: Duration{Duration: 3 * time.Minute}, ConnectedAgents: 500},
		},
		Safety: Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.05},
	}
}

// onlineAfter is count machines that each came online after the given time from their dial.
func onlineAfter(count int, after time.Duration) []time.Duration {
	out := make([]time.Duration, count)
	for i := range out {
		out[i] = after
	}
	return out
}

func TestAMeasuredPhaseCountsItsWaveWhenEachMachineFinishes(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	fleet.wave = Wave{
		Dialled: 1500,
		Pending: 40,
		Online:  append(onlineAfter(1400, time.Second), onlineAfter(100, 8*time.Second)...),
	}
	// The last machines of the wave finish three seconds past the phase's own end.
	fleet.settlesAt = start.Add(2*time.Minute + 30*time.Second + 3*time.Second)

	results, err := RunPhasesWatched(spikeShapedProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)
	require.Len(t, results, 3)

	spike := results[1]
	require.NotNil(t, spike.WaveOnlineShare, "the measured phase says how much of its wave came online")
	assert.InDelta(t, 1.0, *spike.WaveOnlineShare, 0.0001, "every machine dialled came online")
	require.NotNil(t, spike.WaveOnlineP95Ms)
	assert.InDelta(t, 8000.0, *spike.WaveOnlineP95Ms, 0.001,
		"a hundred of fifteen hundred took eight seconds, which is past the 95th percentile")
	assert.Contains(t, fleet.waveAsked, [2]int{500, 2000},
		"the wave is the machines this phase asked for, past the level the baseline left")
	assert.False(t, results[2].StartedAt.Before(fleet.settlesAt),
		"the next phase starts once the wave has finished, so its wind-down cancels none of it")
}

func TestAMeasuredPhaseHoldsStillNoLongerThanAMachinesDeadline(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	fleet.wave = Wave{Dialled: 1500, Pending: 30, Online: onlineAfter(1470, time.Second)}
	// The last thirty machines never finish.
	fleet.settlesAt = start.Add(time.Hour)

	results, err := RunPhasesWatched(spikeShapedProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	spike := results[1]
	require.NotNil(t, spike.WaveOnlineShare)
	assert.InDelta(t, 0.98, *spike.WaveOnlineShare, 0.0001,
		"a machine still dialling at the deadline counts as one that never came online")
	held := results[2].StartedAt.Sub(spike.FinishedAt)
	assert.GreaterOrEqual(t, held, agentDeadline, "the phase waits out a machine's whole deadline")
	assert.Less(t, held, agentDeadline+time.Second, "and no longer")
}

func TestAPhaseThatIsNotMeasuredRecordsNoWave(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	fleet := &recordingFleet{clock: clock}
	fleet.wave = Wave{Dialled: 250, Online: onlineAfter(250, time.Second)}

	results, err := RunPhasesWatched(threePhaseProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	for _, result := range results {
		assert.Nil(t, result.WaveOnlineShare, "%s marks no measured phase", result.Name)
		assert.Nil(t, result.WaveOnlineP95Ms, "%s marks no measured phase", result.Name)
	}
	assert.Empty(t, fleet.waveAsked, "nothing waits on a wave nobody judges")
}

func TestAMeasuredPhaseThatDialledNobodyRecordsNoWave(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	fleet := &recordingFleet{clock: clock}
	profile := onePhaseProfile(0, 10*time.Second)
	profile.Phases[0].Measured = true

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	assert.Nil(t, results[0].WaveOnlineShare, "a wave of nobody has no share to report")
	assert.Nil(t, results[0].WaveOnlineP95Ms)
}
