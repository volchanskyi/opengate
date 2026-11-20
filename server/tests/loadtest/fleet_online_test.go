package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func returnShapedProfile() *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "test",
		Family:        FamilySpike,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "return", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 100},
			{Name: "drain", Duration: Duration{Duration: 10 * time.Minute}, ConnectedAgents: 100, Measured: true},
		},
		Safety: Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.05},
		Gates: []Gate{
			{Series: "quic/online/return", Metric: "online_p95_seconds", Max: ptrFloat(180), Blocking: true},
			{Series: "quic/online/return", Metric: "online_all_seconds", Max: ptrFloat(300), Blocking: true},
		},
	}
}

func ptrFloat(v float64) *float64 { return &v }

// onlineAt is count machines that each came online the given time after start.
func onlineAt(start time.Time, count int, after time.Duration) []time.Time {
	out := make([]time.Time, count)
	for i := range out {
		out[i] = start.Add(after)
	}
	return out
}

func TestAPhaseTimesItsFleetOnlineFromItsOwnStartCountingMachinesAfterItsEnd(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	// Ninety machines are online inside the minute; ten straggle in long after it ends.
	at := append(onlineAt(start, 90, 30*time.Second), onlineAt(start, 5, 100*time.Second)...)
	fleet.wave = Wave{Dialled: 100, OnlineAt: append(at, onlineAt(start, 5, 200*time.Second)...)}

	results, err := RunPhasesWatched(returnShapedProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)
	require.Len(t, results, 2)

	back := results[0]
	require.NotNil(t, back.FleetOnlineP95Seconds)
	assert.InDelta(t, 100.0, *back.FleetOnlineP95Seconds, 0.001,
		"the ninety-fifth machine of a hundred came online 100 s after the return began")
	require.NotNil(t, back.FleetOnlineAllSeconds)
	assert.InDelta(t, 200.0, *back.FleetOnlineAllSeconds, 0.001,
		"the last came online 200 s after it began, well after the phase's own minute")
	assert.Nil(t, results[1].FleetOnlineP95Seconds, "a phase that asked for no machines has no fleet to time")
	assert.Contains(t, fleet.waveAsked, [2]int{0, 100}, "the fleet timed is the machines the phase asked for")
}

func TestOnlyAPhaseItsProfileLimitsHasItsFleetTimed(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	fleet.wave = Wave{Dialled: 100, OnlineAt: onlineAt(start, 100, 30*time.Second)}
	profile := returnShapedProfile()
	profile.Gates = nil

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)
	assert.Nil(t, results[0].FleetOnlineP95Seconds, "a figure no limit names would be a row read against nothing")
	assert.Nil(t, results[0].FleetOnlineAllSeconds)
}

func TestAFleetWithAMachineNeverOnlineHasNoTimeForItsWholeFleet(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	fleet.wave = Wave{Dialled: 100, OnlineAt: onlineAt(start, 99, 40*time.Second)}

	results, err := RunPhasesWatched(returnShapedProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	back := results[0]
	require.NotNil(t, back.FleetOnlineP95Seconds)
	assert.InDelta(t, 40.0, *back.FleetOnlineP95Seconds, 0.001)
	assert.Nil(t, back.FleetOnlineAllSeconds,
		"a machine that never came online leaves the whole fleet's time unread, which fails its limit")

	clock = &testClock{now: start}
	fleet = &recordingFleet{clock: clock}
	fleet.wave = Wave{Dialled: 100, OnlineAt: onlineAt(start, 94, 40*time.Second)}
	results, err = RunPhasesWatched(returnShapedProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)
	assert.Nil(t, results[0].FleetOnlineP95Seconds, "fewer than ninety-five in a hundred online leaves the 95% time unread")
}

// answeredInTurn is count machines dialled every dialEvery from first, answered one per answerEvery
// in dial order, as a line serving them at that pace would.
func answeredInTurn(first time.Time, count int, dialEvery, answerEvery time.Duration) Wave {
	var wave Wave
	for i := range count {
		dial := first.Add(time.Duration(i) * dialEvery)
		answered := first.Add(time.Duration(i+1) * answerEvery)
		wave.Online = append(wave.Online, answered.Sub(dial))
		wave.OnlineAt = append(wave.OnlineAt, answered)
	}
	wave.Dialled = count
	return wave
}

func TestAPhaseReadsItsSignInRateOnlyWhileAMachineWaited(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	// Two hundred dial over the first second and are answered one every 10 ms, so the server is
	// busy for 2 s; ten more dial 10 s in, answered the same way, busy for 0.1 s.
	busy := answeredInTurn(start, 200, 5*time.Millisecond, 10*time.Millisecond)
	later := answeredInTurn(start.Add(10*time.Second), 10, time.Millisecond, 10*time.Millisecond)
	fleet.wave = Wave{
		Dialled:  210,
		Online:   append(busy.Online, later.Online...),
		OnlineAt: append(busy.OnlineAt, later.OnlineAt...),
	}
	profile := returnShapedProfile()
	profile.Gates = append(profile.Gates,
		Gate{Series: "quic/online/return", Metric: "sign_ins_per_second", Min: ptrFloat(100), Blocking: true})

	results, err := RunPhasesWatched(profile, fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)

	rate := results[0].FleetSignInsPerSecond
	require.NotNil(t, rate)
	assert.InDelta(t, 100.0, *rate, 0.5,
		"210 sign-ins over the 2.1 s some machine waited; the idle 8 s between count for nothing")
}

func TestAPhaseWithNoMachineOnlineHasNoSignInRate(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	fleet.wave = Wave{Dialled: 100}

	results, err := RunPhasesWatched(returnShapedProfile(), fleet, clock, alwaysRoomToRun, unreadTarget)
	require.NoError(t, err)
	assert.Nil(t, results[0].FleetSignInsPerSecond, "no sign-in finished, so there is no rate to read")
}
