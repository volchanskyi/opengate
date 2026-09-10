package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shape a built fleet has, and what it obliges the run to remove.

func TestFileDevicesSpreadsThemAcrossTheCustomersThePlanDeclared(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)

	deviceIDs := make([]string, 12)
	for i := range deviceIDs {
		deviceIDs[i] = fmt.Sprintf("device-%02d", i)
	}
	require.NoError(t, fleet.client.FileDevices(fleet.fixture, deviceIDs))

	assert.Len(t, fleet.api.filedToCustomer, len(deviceIDs), "every machine is filed under a customer")

	// The lopsided fleet is the point of the third size: one customer holds most
	// of it, which is the shape a customer-scoped page is actually asked for.
	assert.Greater(t, fleet.fixture.Customers[0].Devices, fleet.fixture.Customers[1].Devices)
}

// Filing a machine under a customer is half of it, and the half nothing reads.
//
// The browser-side scenarios narrow by site: one of them walks every site
// looking for one that holds machines and falls back to the whole-tenant read
// when none does, and the other reads a random site's machines every iteration.
// A fleet filed under customers and into no site leaves the first timing a read
// it was not written to time and the second timing an empty result — so the
// site is not a refinement of the filing, it is what makes the filing legible.
func TestAFiledMachineLandsInASiteOfTheCustomerHoldingIt(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)

	deviceIDs := make([]string, 12)
	for i := range deviceIDs {
		deviceIDs[i] = fmt.Sprintf("device-%02d", i)
	}
	require.NoError(t, fleet.client.FileDevices(fleet.fixture, deviceIDs))

	require.Len(t, fleet.api.filedToSite, len(deviceIDs), "every machine is filed into a site")

	// A site belongs to one customer, so a machine filed into a site its
	// customer does not hold is a machine the server refuses — and the run would
	// find out one request at a time, in the middle of its ramp.
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

// A customer with several sites holds machines in more than one of them.
//
// Every machine landing in a customer's first site would file the estate
// truthfully and still leave the other sites empty, which is the same empty
// read the scenarios get today wearing a different shape.
func TestACustomersMachinesAreSpreadAcrossTheSitesItHas(t *testing.T) {
	fleet := buildFleet(t, FixtureLopsided, 3)

	// Enough machines that the majority customer's sites can each take some.
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

func TestBuiltFixtureCarriesTheManifestOfWhatToRemove(t *testing.T) {
	fleet := buildFleet(t, FixtureSmall, 5)

	manifest := fleet.fixture.CleanupManifest()
	assert.Equal(t, loadTestMarker, manifest.Marker)
	assert.Len(t, manifest.Users, len(fleet.plan.Users))
	assert.Equal(t, fleet.plan.Devices, manifest.Devices)
	assert.NotEmpty(t, manifest.Organizations, "a customer a run created is one it must remove")
}

func TestFixtureCountsDescribeWhatWasActuallyCreated(t *testing.T) {
	fleet := buildFleet(t, FixtureLarge, 11)

	counts := fleet.fixture.Counts()
	assert.Equal(t, FixtureLarge, counts.Size)
	assert.Equal(t, len(fleet.plan.Customers), counts.Customers)
	assert.Equal(t, fleet.plan.Sites, counts.Sites)
	assert.Equal(t, len(fleet.plan.Users), counts.Users)
	// The plan under its own name. The machines themselves arrive by enrolling,
	// which happens after the fixture is built, so the fleet that exists is
	// counted by whoever counted the arrivals rather than claimed here.
	assert.Equal(t, fleet.plan.Devices, counts.PlannedDevices)
	assert.Zero(t, counts.Devices, "a fixture that has built no machines has none")
	assert.Equal(t, 1, counts.Tenants, "there is no way to ask for a second tenant yet")
}

// The same seed reproduces the same fleet exactly, or two runs differ for
// reasons nobody can separate afterwards.
func TestBuildFixtureIsReproducibleFromItsSeed(t *testing.T) {
	first := buildFleet(t, FixtureLopsided, 42)
	second := buildFleet(t, FixtureLopsided, 42)

	assert.Equal(t, first.api.organizations, second.api.organizations)
	assert.Equal(t, first.api.sites, second.api.sites)
	assert.Equal(t, first.api.registered, second.api.registered)
}
