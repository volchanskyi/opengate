package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// failFilingAt makes the fake server refuse requests to exactly that path; empty clears it.
func failFilingAt(api *fakeAPI, path string) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.failAt = path
}

const devicesPath = "/api/v1/devices/"

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

func TestAFilingTheServerRefusesIsCountedRatherThanFatal(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	failFilingAt(api, devicesPath)

	filer.file(context.Background(), aMachine(0))

	filed, refused := filer.counts()
	assert.Zero(t, filed, "a machine the server refused to file is not a filed machine")
	assert.Equal(t, 1, refused)
}

func TestTheEstateAnnouncesItselfFiledOnceTheDeclaredLevelIsReached(t *testing.T) {
	filer, _ := aFiler(t, 10, 3)

	assert.False(t, filer.file(context.Background(), aMachine(0)), "one of three is not the level")
	assert.False(t, filer.file(context.Background(), aMachine(1)), "two of three is not the level")
	assert.True(t, filer.file(context.Background(), aMachine(2)), "the third machine reaches it")

	assert.False(t, filer.file(context.Background(), aMachine(3)), "the level is announced once")
}

func TestRefusedFilingsDoNotReachTheAnnouncedLevel(t *testing.T) {
	filer, api := aFiler(t, 10, 2)
	failFilingAt(api, devicesPath)

	assert.False(t, filer.file(context.Background(), aMachine(0)))
	assert.False(t, filer.file(context.Background(), aMachine(1)))
	assert.False(t, filer.file(context.Background(), aMachine(2)))
}

func TestARunWithNoFixtureFilesNothing(t *testing.T) {
	var filer *estateFiler
	assert.NotPanics(t, func() {
		assert.False(t, filer.file(context.Background(), aMachine(0)))
	})
	filed, refused := filer.counts()
	assert.Zero(t, filed)
	assert.Zero(t, refused)
}

func TestAnArrivalBothCountsAndFiles(t *testing.T) {
	filer, api := aFiler(t, 10, 10)
	machine := aMachine(0)

	counted := 0
	arrivalOf(func() { counted++ }, filer, machine)()

	assert.Equal(t, 1, counted, "the phase still counts the arrival")
	assert.Len(t, api.filedToCustomer, 1, "the estate files the machine that arrived")
}

func TestAnArrivalWithNobodyWatchingIsHarmless(t *testing.T) {
	assert.NotPanics(t, func() { arrivalOf(nil, nil, aMachine(0))() })
}

func TestTheAnnouncedLevelIsTheFirstPhasesOwn(t *testing.T) {
	profile := &Profile{Phases: []Phase{
		{Name: "ramp", ConnectedAgents: 250},
		{Name: "steady", ConnectedAgents: 500},
	}}
	assert.Equal(t, 250, filingLevel(profile, 500))

	assert.Equal(t, 500, filingLevel(nil, 500))

	assert.Equal(t, 500, filingLevel(&Profile{Phases: []Phase{{Name: "quiet"}}}, 500))
}

func TestAFleetTheRunCannotFileStopsBeingAskedFor(t *testing.T) {
	filer, api := aFiler(t, 100, 100)
	failFilingAt(api, devicesPath)

	for i := 0; i < 40; i++ {
		filer.file(context.Background(), aMachine(i))
	}

	_, refused := filer.counts()
	assert.Positive(t, refused, "the run tried")
	assert.LessOrEqual(t, refused, filingAttemptsBeforeGivingUp,
		"a run that cannot file stops asking rather than spending an arrival's allowance per machine")
}

func TestOneRefusalDoesNotStopTheRunFiling(t *testing.T) {
	filer, api := aFiler(t, 10, 10)

	failFilingAt(api, devicesPath)
	filer.file(context.Background(), aMachine(0))

	failFilingAt(api, "")
	filer.file(context.Background(), aMachine(1))

	filed, refused := filer.counts()
	assert.Equal(t, 1, filed, "the machine after the refusal was still filed")
	assert.Equal(t, 1, refused)
}

func TestARunThatHasFiledKeepsTryingHoweverManyAreRefused(t *testing.T) {
	filer, api := aFiler(t, 100, 100)
	filer.file(context.Background(), aMachine(0))

	failFilingAt(api, devicesPath)
	for i := 1; i < 40; i++ {
		filer.file(context.Background(), aMachine(i))
	}

	filed, refused := filer.counts()
	assert.Equal(t, 1, filed)
	assert.Equal(t, 39, refused, "every one of them was tried")
}
