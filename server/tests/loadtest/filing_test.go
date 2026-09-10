package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Filing an estate as it arrives.
//
// A machine can only be filed once its row exists, and the row exists when the
// machine registers — so the filing follows each arrival rather than waiting for
// the last one. What the run holds beforehand is enough to do it without asking
// the server anything: the plan says which customer this machine belongs to, and
// the credential it dials with carries its identifier.

// aFiler builds a filer over a fake server, with an estate of the given size.
func aFiler(t *testing.T, estate, readyAt int) (*estateFiler, *fakeAPI) {
	t.Helper()

	fleet := buildFleet(t, FixtureLopsided, 3)
	source, err := newAgentCredentials(t.TempDir(), "", "")
	require.NoError(t, err)

	return newEstateFiler(fleet.client, fleet.fixture, enrolOnce(source), estate, readyAt), fleet.api
}

// aMachine is one machine at a given place in the estate.
func aMachine(index int) tenantAgent {
	return tenantAgent{agentIndex: index, hostname: fmt.Sprintf("soak-t0-a%d", index)}
}

func TestAnArrivedMachineIsFiledUnderItsCustomerAndIntoASite(t *testing.T) {
	filer, api := aFiler(t, 10, 10)

	filer.file(context.Background(), aMachine(0))

	require.Len(t, api.filedToCustomer, 1, "an arrived machine is filed under its customer")
	require.Len(t, api.filedToSite, 1, "an arrived machine is filed into a building")
	assert.Equal(t, api.filedToCustomer[0].deviceID, api.filedToSite[0].deviceID,
		"both halves file the same machine")

	filed, refused := filer.counts()
	assert.Equal(t, 1, filed)
	assert.Zero(t, refused)
}

// A machine that comes back after an outage is the same machine, so filing it
// again would be a second write for a row that is already right. It is also how
// a five-hour run with ten cycles of churn would turn one filing into ten.
func TestAMachineThatComesBackIsNotFiledAgain(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	machine := aMachine(0)

	filer.file(context.Background(), machine)
	filer.file(context.Background(), machine)
	filer.file(context.Background(), machine)

	assert.Len(t, api.filedToCustomer, 1, "a machine returning is a machine already filed")
	filed, _ := filer.counts()
	assert.Equal(t, 1, filed)
}

// A refusal is counted and the machine carries on.
//
// Filing is not the load, and a run that died because one filing was refused
// would throw away the measurement it had already taken. What must not happen is
// the opposite — a run that could not file its estate reporting a filed one —
// so the count travels and the run says what it managed.
func TestAFilingTheServerRefusesIsCountedRatherThanFatal(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	api.mu.Lock()
	api.failAt = "/api/v1/devices/"
	api.mu.Unlock()

	filer.file(context.Background(), aMachine(0))

	filed, refused := filer.counts()
	assert.Zero(t, filed, "a machine the server refused to file is not a filed machine")
	assert.Equal(t, 1, refused)
}

// The run announces a filed estate once, at the level it declared.
//
// The browser-side scenarios pick the building they will time in their own
// setup, once, before their first iteration — so a scenario started before any
// machine is filed reads an empty building for its whole run, whatever gets
// filed afterwards. The announcement is what the step ordering hangs off.
func TestTheEstateAnnouncesItselfFiledOnceTheDeclaredLevelIsReached(t *testing.T) {
	filer, _ := aFiler(t, 10, 3)

	assert.False(t, filer.file(context.Background(), aMachine(0)), "one of three is not the level")
	assert.False(t, filer.file(context.Background(), aMachine(1)), "two of three is not the level")
	assert.True(t, filer.file(context.Background(), aMachine(2)), "the third machine reaches it")

	// Once, not once per arrival after it: a step waiting on the line would
	// otherwise be told the same thing hundreds of times.
	assert.False(t, filer.file(context.Background(), aMachine(3)), "the level is announced once")
}

// A refused filing does not count toward the level, or a run that filed nothing
// would announce a filed estate.
func TestRefusedFilingsDoNotReachTheAnnouncedLevel(t *testing.T) {
	filer, api := aFiler(t, 10, 2)
	api.mu.Lock()
	api.failAt = "/api/v1/devices/"
	api.mu.Unlock()

	assert.False(t, filer.file(context.Background(), aMachine(0)))
	assert.False(t, filer.file(context.Background(), aMachine(1)))
	assert.False(t, filer.file(context.Background(), aMachine(2)))
}

// A run that built no fixture has no customers to file under, and that is an
// ordinary shape rather than a failure: a bare run against a local stack brings
// its own machines and nobody to file them for.
func TestARunWithNoFixtureFilesNothing(t *testing.T) {
	var filer *estateFiler
	assert.NotPanics(t, func() {
		assert.False(t, filer.file(context.Background(), aMachine(0)))
	})
	filed, refused := filer.counts()
	assert.Zero(t, filed)
	assert.Zero(t, refused)
}

// The two things that happen when a machine arrives, composed once.
//
// The fleet counts the arrival in the phase it happened in, and the estate files
// the machine. Both hang off the same moment — the machine is registered, so its
// row exists — and naming the pair is what lets a test say the filing is still
// wired to it.
func TestAnArrivalBothCountsAndFiles(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	machine := aMachine(0)

	counted := 0
	arrivalOf(func() { counted++ }, filer, machine)()

	assert.Equal(t, 1, counted, "the phase still counts the arrival")
	assert.Len(t, api.filedToCustomer, 1, "the estate files the machine that arrived")
}

// A run with nobody keeping a tally and nothing to file against is the ordinary
// bare run, and it must not fall over on either half being absent.
func TestAnArrivalWithNobodyWatchingIsHarmless(t *testing.T) {
	assert.NotPanics(t, func() { arrivalOf(nil, nil, aMachine(0))() })
}

// How many filed machines make the estate worth announcing.
//
// It is the first phase's level rather than the whole estate: a profile climbs,
// so the estate is only complete once the tallest phase is reached, and a step
// waiting for that would stand idle through the ramp it was meant to overlap.
// What the scenarios need is a building that holds machines, and the first
// phase's level is the first moment the run can promise one.
func TestTheAnnouncedLevelIsTheFirstPhasesOwn(t *testing.T) {
	profile := &Profile{Phases: []Phase{
		{Name: "ramp", ConnectedAgents: 250},
		{Name: "steady", ConnectedAgents: 500},
	}}
	assert.Equal(t, 250, filingLevel(profile, 500))

	// A run with no profile offers every machine at once, so the estate is the
	// level and there is no earlier moment to name.
	assert.Equal(t, 500, filingLevel(nil, 500))

	// A first phase that connects nobody is a profile that starts idle; the
	// estate is the honest answer rather than nought, which would announce a
	// filed estate before anything had arrived.
	assert.Equal(t, 500, filingLevel(&Profile{Phases: []Phase{{Name: "quiet"}}}, 500))
}
