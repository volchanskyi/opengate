package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileDevicesSpreadsThemAcrossTheCustomersThePlanDeclared(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)

	deviceIDs := make([]string, 12)
	for i := range deviceIDs {
		deviceIDs[i] = fmt.Sprintf("device-%02d", i)
	}
	require.NoError(t, fleet.client.FileDevices(fleet.fixture, deviceIDs))

	assert.Len(t, fleet.api.filedToCustomer, len(deviceIDs), "every machine is filed under a customer")

	assert.Greater(t, fleet.fixture.Customers[0].Devices, fleet.fixture.Customers[1].Devices)
}

func TestAFiledMachineLandsInASiteOfTheCustomerHoldingIt(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)

	deviceIDs := make([]string, 12)
	for i := range deviceIDs {
		deviceIDs[i] = fmt.Sprintf("device-%02d", i)
	}
	require.NoError(t, fleet.client.FileDevices(fleet.fixture, deviceIDs))

	require.Len(t, fleet.api.filedToSite, len(deviceIDs), "every machine is filed into a site")

	siteOwner := map[string]string{}
	for _, customer := range fleet.fixture.Customers {
		for _, site := range customer.SiteIDs {
			siteOwner[site] = customer.ID
		}
	}
	customerOf := map[string]string{}
	for _, filed := range fleet.api.filedToCustomer {
		customerOf[filed.deviceID] = filed.target
	}

	for _, filed := range fleet.api.filedToSite {
		require.NotEmpty(t, filed.target, "machine %s was filed into no site", filed.deviceID)
		assert.Equal(t, customerOf[filed.deviceID], siteOwner[filed.target],
			"machine %s went into a site its customer does not hold", filed.deviceID)
	}
}

func TestACustomersMachinesAreSpreadAcrossTheSitesItHas(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)

	deviceIDs := make([]string, 200)
	for i := range deviceIDs {
		deviceIDs[i] = fmt.Sprintf("device-%03d", i)
	}
	require.NoError(t, fleet.client.FileDevices(fleet.fixture, deviceIDs))

	majority := fleet.fixture.Customers[0]
	require.Greater(t, len(majority.SiteIDs), 1, "the majority customer should have several sites")

	held := map[string]bool{}
	for _, filed := range fleet.api.filedToSite {
		held[filed.target] = true
	}
	used := 0
	for _, site := range majority.SiteIDs {
		if held[site] {
			used++
		}
	}
	assert.Greater(t, used, 1, "the majority customer's machines all landed in one of its %d sites", len(majority.SiteIDs))
}

func TestFixtureCountsDescribeWhatWasActuallyCreated(t *testing.T) {
	fleet := buildFleet(t, FixtureLarge, 11)

	counts := fleet.fixture.Counts()
	assert.Equal(t, FixtureLarge, counts.Size)
	assert.Equal(t, len(fleet.plan.Customers), counts.Customers)
	assert.Equal(t, fleet.plan.Sites, counts.Sites)
	assert.Equal(t, len(fleet.plan.Users), counts.Users)
	assert.Equal(t, fleet.plan.Devices, counts.PlannedDevices)
	assert.Zero(t, counts.Devices, "a fixture that has built no machines has none")
	assert.Equal(t, 1, counts.Tenants, "there is no way to ask for a second tenant yet")
}

func TestBuildFixtureIsReproducibleFromItsSeed(t *testing.T) {
	first := buildFleet(t, FixtureLopsided, 42)
	second := buildFleet(t, FixtureLopsided, 42)

	assert.Equal(t, first.api.organizations, second.api.organizations)
	assert.Equal(t, first.api.sites, second.api.sites)
	assert.Equal(t, first.api.registered, second.api.registered)
}
