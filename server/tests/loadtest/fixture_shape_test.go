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

	assert.Len(t, fleet.api.filedDevices, len(deviceIDs), "every machine is filed under a customer")

	// The lopsided fleet is the point of the third size: one customer holds most
	// of it, which is the shape a customer-scoped page is actually asked for.
	assert.Greater(t, fleet.fixture.Customers[0].Devices, fleet.fixture.Customers[1].Devices)
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
	assert.Equal(t, fleet.plan.Devices, counts.Devices)
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
