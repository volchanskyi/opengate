package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func estateOf(t *testing.T, n int) *agentRoster {
	t.Helper()
	plan := make([]tenantAgent, n)
	for i := range plan {
		plan[i] = tenantAgent{agentIndex: i, hostname: hostnameFor(i)}
	}
	return newAgentRoster(plan)
}

func hostnameFor(i int) string {
	return "soak-t0-a" + string(rune('0'+i%10)) + "-" + string(rune('a'+i/10))
}

func TestAMachineThatLeftComesBackAsItself(t *testing.T) {
	roster := estateOf(t, 1)

	first, giveBack, ok := roster.take()
	require.True(t, ok)
	giveBack()

	again, _, ok := roster.take()
	require.True(t, ok)
	assert.Equal(t, first.hostname, again.hostname,
		"a machine that reconnects is the same machine, so it dials with the identity it already holds")
}

func TestTwoLiveMachinesAreNeverTheSameOne(t *testing.T) {
	roster := estateOf(t, 3)

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		machine, _, ok := roster.take()
		require.True(t, ok)
		assert.False(t, seen[machine.hostname],
			"one device with two live connections is the server keeping whichever registered last")
		seen[machine.hostname] = true
	}
}

func TestAnEstateWithNobodyFreeSaysSo(t *testing.T) {
	roster := estateOf(t, 1)

	_, _, ok := roster.take()
	require.True(t, ok)

	_, _, ok = roster.take()
	assert.False(t, ok, "a machine that cannot be given an identity has not arrived, and saying so is the finding")
}

func TestGivingAMachineBackPutsItWithinReachAgain(t *testing.T) {
	roster := estateOf(t, 1)

	_, giveBack, ok := roster.take()
	require.True(t, ok)

	_, _, ok = roster.take()
	require.False(t, ok)

	giveBack()
	_, _, ok = roster.take()
	assert.True(t, ok)
}

func TestGivingTheSameMachineBackTwiceIsOnce(t *testing.T) {
	roster := estateOf(t, 1)

	_, giveBack, ok := roster.take()
	require.True(t, ok)
	giveBack()
	giveBack()

	_, _, ok = roster.take()
	require.True(t, ok)
	_, _, ok = roster.take()
	assert.False(t, ok, "one machine given back twice is still one machine")
}

func TestAFleetStartingAtOnceStillGetsDistinctMachines(t *testing.T) {
	const size = 64
	roster := estateOf(t, size)

	var mu sync.Mutex
	seen := map[string]int{}

	var wg sync.WaitGroup
	for i := 0; i < size*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			machine, _, ok := roster.take()
			if !ok {
				return
			}
			mu.Lock()
			seen[machine.hostname]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	assert.Len(t, seen, size, "every machine in the estate was handed out")
	for hostname, times := range seen {
		assert.Equal(t, 1, times, "%s was handed out more than once", hostname)
	}
}

func TestAnEstateReportsHowManyMachinesItHolds(t *testing.T) {
	assert.Equal(t, 4, estateOf(t, 4).size())
}

func TestAProfileAskingForMoreMachinesThanTheEstateHoldsIsRefused(t *testing.T) {
	profile := &Profile{Phases: []Phase{
		{Name: "baseline", ConnectedAgents: 500},
		{Name: "spike", ConnectedAgents: 2000},
	}}

	err := checkEstateHolds(profile, 500)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2000")
	assert.Contains(t, err.Error(), "spike", "the refusal names the phase that cannot be reached")
}

func TestAnEstateThatCoversEveryPhaseIsAccepted(t *testing.T) {
	profile := &Profile{Phases: []Phase{
		{Name: "ramp", ConnectedAgents: 100},
		{Name: "steady", ConnectedAgents: 500},
		{Name: "drain", ConnectedAgents: 0},
	}}

	assert.NoError(t, checkEstateHolds(profile, 500))
}

func TestARunWithNoProfileNeedsNoEstateCheck(t *testing.T) {
	assert.NoError(t, checkEstateHolds(nil, 0))
}

func TestAMachineStartedAgainReconnectsRatherThanEnrolling(t *testing.T) {
	source := &countingSource{}
	roster := estateOf(t, 1)
	start := estateStart(roster, enrolOnce(source), "127.0.0.1:1", loadOptions{}, nil)

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		res := start(ctx, i, fleetPresence{})
		cancel()
		// Reaching the dial shows the machine was given back after the previous start.
		require.NotErrorIs(t, res.err, ErrEstateExhausted, "start %d was refused a machine", i)
	}

	assert.Len(t, source.askedFor(), 1,
		"a burst of reconnections must not be a burst of enrolments against a ceiling the server enforces on purpose")
}

func TestAStartWithNobodyFreeReportsThatRatherThanDoublingUp(t *testing.T) {
	roster := estateOf(t, 1)
	_, _, ok := roster.take()
	require.True(t, ok)

	start := estateStart(roster, enrolOnce(&countingSource{}), "127.0.0.1:1", loadOptions{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	assert.ErrorIs(t, start(ctx, 0, fleetPresence{}).err, ErrEstateExhausted)
}
