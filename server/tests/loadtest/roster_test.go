package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The estate is fixed and its machines come and go. A profile that winds a
// level down and back up is machines losing their connections and getting them
// back, so what the roster has to guarantee is that the machine that comes back
// is one of the ones that left — and that no two live connections are ever the
// same machine.

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

// An estate with nobody free says so. Doubling up would put one device on two
// connections at once, which is a worse fault than the re-enrolment this
// closes: the server keeps the newer connection and the level silently drops.
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

// Giving the same machine back twice must not put two copies of it in reach —
// a fleet winding down races its own machines' returns, and the second give
// would hand the same identity to two starts.
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

// A ramp starts its machines together, so the roster is asked from many
// goroutines at once. Run under -race this is the case that would show two
// starts sharing one identity.
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

// The estate a run draws from is sized by -agents, and a profile that asks for
// more machines than it holds cannot be walked: the level it declares is a
// level the run can never reach, and every step past the estate's size is a
// machine that could not arrive.
//
// It is refused before the clock starts rather than discovered as a fleet that
// would not climb, because the two look identical in a bundle — an attainment
// short of its offer — and only one of them is a finding about the system.
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

// A run with no profile offers every machine it was given at once, so there is
// no declared level to check against.
func TestARunWithNoProfileNeedsNoEstateCheck(t *testing.T) {
	assert.NoError(t, checkEstateHolds(nil, 0))
}

// What D30 is actually about, assembled: a machine that leaves and is started
// again is the same machine, and it enrols once however many times the run
// brings it back.
//
// The dial goes nowhere — a closed port on the loopback — because what is being
// counted here is enrolments, and enrolment happens before the machine dials.
// Three starts of a one-machine estate is a machine reconnecting twice.
func TestAMachineStartedAgainReconnectsRatherThanEnrolling(t *testing.T) {
	source := &countingSource{}
	roster := estateOf(t, 1)
	start := estateStart(roster, enrolOnce(source), "127.0.0.1:1", loadOptions{})

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		res := start(ctx, i, nil)
		cancel()
		// Every start got as far as dialling, which is what says the machine was
		// given back after the one before it. A start that had been refused an
		// identity would also have enrolled once, and would prove nothing.
		require.NotErrorIs(t, res.err, ErrEstateExhausted, "start %d was refused a machine", i)
	}

	assert.Len(t, source.askedFor(), 1,
		"a burst of reconnections must not be a burst of enrolments against a ceiling the server enforces on purpose")
}

// A start that could not be given an identity is a machine that did not arrive,
// and it says which — the alternative is one device on two live connections,
// where the server keeps whichever registered last and the level drops with
// nothing reporting it.
func TestAStartWithNobodyFreeReportsThatRatherThanDoublingUp(t *testing.T) {
	roster := estateOf(t, 1)
	_, _, ok := roster.take()
	require.True(t, ok)

	start := estateStart(roster, enrolOnce(&countingSource{}), "127.0.0.1:1", loadOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	assert.ErrorIs(t, start(ctx, 0, nil).err, ErrEstateExhausted)
}
