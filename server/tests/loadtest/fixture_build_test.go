package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFixtureCreatesEveryCustomerAndSiteThePlanDeclares(t *testing.T) {
	plan, err := PlanFixture(FixtureSmall, 7)
	require.NoError(t, err)

	api := &fakeAPI{}
	client := newFixtureClient(t, api)
	require.NoError(t, client.SignIn("admin@service.invalid", "secret"))

	built, err := client.BuildFixture(plan)
	require.NoError(t, err)

	assert.Len(t, api.organizations, len(plan.Customers), "one customer per plan row")
	assert.Len(t, api.sites, plan.Sites, "one site per plan row")
	assert.Len(t, api.registered, len(plan.Users), "one account per plan row")
	assert.Equal(t, "enrol-secret", built.EnrollmentToken, "the machines need a way in")
	assert.Equal(t, plan.Devices, built.PlannedDevices)

	// The names are the plan's, so what a run left behind can be recognised.
	for _, name := range api.organizations {
		assert.True(t, strings.HasPrefix(name, loadTestMarker), "customer %q carries no marker", name)
	}
}

func TestBuildFixtureStopsAtTheFirstRefusal(t *testing.T) {
	plan, err := PlanFixture(FixtureSmall, 1)
	require.NoError(t, err)

	api := &fakeAPI{failAt: "/api/v1/sites"}
	client := newFixtureClient(t, api)
	require.NoError(t, client.SignIn("admin@service.invalid", "secret"))

	_, err = client.BuildFixture(plan)
	require.Error(t, err)
	// Half a fixture measured against is worse than none: the numbers look
	// ordinary and describe a fleet nobody declared.
	assert.Contains(t, err.Error(), "site")
}

func TestBuildFixtureNeedsASession(t *testing.T) {
	plan, err := PlanFixture(FixtureSmall, 1)
	require.NoError(t, err)

	api := &fakeAPI{}
	client := newFixtureClient(t, api)

	_, err = client.BuildFixture(plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sign in")
}

// D35: the credential the fleet enrols with has to outlive the run that spends
// it. It asked for one hour, and every machine a phase starts calls the
// enrolment endpoint — so a profile past its first hour was refused from there
// on, and a five-hour soak would have enrolled nobody after the first sixty
// minutes.
func TestTheEnrolmentCredentialOutlivesTheRunThatSpendsIt(t *testing.T) {
	plan, err := PlanFixture(FixtureSmall, 1)
	require.NoError(t, err)

	api := &fakeAPI{}
	client := newFixtureClientForRun(t, api, 5*time.Hour)
	require.NoError(t, client.SignIn("admin@service.invalid", "secret"))

	_, err = client.BuildFixture(plan)
	require.NoError(t, err)

	hours := api.hoursAsked()
	require.Len(t, hours, 1)
	assert.GreaterOrEqual(t, hours[0], 6,
		"the credential covers the walk and the fixture build that precedes it")
}

// A run that declares nothing still gets a credential that lives an hour, which
// is what the everyday nightly has always had.
func TestARunThatDeclaresNoLengthStillGetsAnHour(t *testing.T) {
	assert.Equal(t, 1, enrollmentTokenHours(0))
}

// The hours are whole, so a run that runs into the next hour by a minute gets
// the whole of it rather than being cut off inside it.
func TestAPartHourIsRoundedUpRatherThanTruncated(t *testing.T) {
	assert.Equal(t, 3, enrollmentTokenHours(70*time.Minute))
	assert.Equal(t, 6, enrollmentTokenHours(5*time.Hour))
}
