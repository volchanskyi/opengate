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

func TestARunThatDeclaresNoLengthStillGetsAnHour(t *testing.T) {
	assert.Equal(t, 1, enrollmentTokenHours(0))
}

func TestAPartHourIsRoundedUpRatherThanTruncated(t *testing.T) {
	assert.Equal(t, 3, enrollmentTokenHours(70*time.Minute))
	assert.Equal(t, 6, enrollmentTokenHours(5*time.Hour))
}
