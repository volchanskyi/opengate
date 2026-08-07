package device_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestListNarrowsByOrganizationAndFallsBackToTheTenant is the read rule: a
// selected customer narrows the list, and no selection returns the whole tenant.
func TestListNarrowsByOrganizationAndFallsBackToTheTenant(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	contoso := newCustomer(t, ctx, store, "Contoso")
	fabrikam := newCustomer(t, ctx, store, "Fabrikam")

	inContoso := testutil.SeedDevice(t, ctx, store, site.ID)
	inFabrikam := testutil.SeedDevice(t, ctx, store, site.ID)
	require.NoError(t, devices.UpdateOrganization(ctx, inContoso.ID, contoso))
	require.NoError(t, devices.UpdateOrganization(ctx, inFabrikam.ID, fabrikam))

	narrowed, err := devices.List(ctx, device.Filter{OrganizationID: contoso})
	require.NoError(t, err)
	require.Len(t, narrowed, 1)
	assert.Equal(t, inContoso.ID, narrowed[0].ID)

	whole, err := devices.List(ctx, device.Filter{})
	require.NoError(t, err)
	assert.Len(t, whole, 2, "no customer selected returns the whole tenant")

	// Site and customer narrow together rather than one replacing the other. The
	// office has to be one of Fabrikam's own, since a move unfiles the machine.
	fabrikamSite := &device.Site{ID: uuid.New(), OrganizationID: fabrikam, Name: "Austin"}
	require.NoError(t, testutil.NewTestSites(t, store).Create(ctx, fabrikamSite))
	require.NoError(t, devices.UpdateSite(ctx, inFabrikam.ID, fabrikamSite.ID))

	both, err := devices.List(ctx, device.Filter{SiteID: fabrikamSite.ID, OrganizationID: fabrikam})
	require.NoError(t, err)
	require.Len(t, both, 1)
	assert.Equal(t, inFabrikam.ID, both[0].ID)
}

// TestCountsNarrowByOrganization proves the fleet rollup answers for the
// selected customer, so the dashboard tiles and the device list describe the same
// set.
func TestCountsNarrowByOrganization(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	contoso := newCustomer(t, ctx, store, "Contoso")
	fabrikam := newCustomer(t, ctx, store, "Fabrikam")

	one := testutil.SeedDevice(t, ctx, store, site.ID)
	two := testutil.SeedDevice(t, ctx, store, site.ID)
	three := testutil.SeedDevice(t, ctx, store, site.ID)
	require.NoError(t, devices.UpdateOrganization(ctx, one.ID, contoso))
	require.NoError(t, devices.UpdateOrganization(ctx, two.ID, contoso))
	require.NoError(t, devices.UpdateOrganization(ctx, three.ID, fabrikam))

	narrowed, err := devices.Counts(ctx, contoso)
	require.NoError(t, err)
	assert.Equal(t, 2, narrowed.Total)

	whole, err := devices.Counts(ctx, uuid.Nil)
	require.NoError(t, err)
	assert.Equal(t, 3, whole.Total)
}

// TestDeletingAnOrganizationTakesItsDevices proves the erasure cascade at the
// customer level: the devices go, and so does everything keyed to them.
func TestDeletingAnOrganizationTakesItsDevices(t *testing.T) {
	t.Parallel()
	devices, _, hardware, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	orgs := testutil.NewTestOrganizations(t, store)

	site := testutil.SeedSite(t, ctx, store)
	doomed := newCustomer(t, ctx, store, "Doomed")
	survivor := newCustomer(t, ctx, store, "Survivor")

	inDoomed := testutil.SeedDevice(t, ctx, store, site.ID)
	inSurvivor := testutil.SeedDevice(t, ctx, store, site.ID)
	require.NoError(t, devices.UpdateOrganization(ctx, inDoomed.ID, doomed))
	require.NoError(t, devices.UpdateOrganization(ctx, inSurvivor.ID, survivor))
	require.NoError(t, hardware.Upsert(ctx, &device.Hardware{DeviceID: inDoomed.ID, CPUModel: "gone"}))

	require.NoError(t, orgs.Delete(ctx, doomed))

	_, err := devices.Get(ctx, inDoomed.ID)
	assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	_, err = hardware.Get(ctx, inDoomed.ID)
	assert.ErrorIs(t, err, device.ErrHardwareNotFound, "nothing keyed to the device is left orphaned")

	_, err = devices.Get(ctx, inSurvivor.ID)
	assert.NoError(t, err, "the other customer's fleet is untouched")
}
