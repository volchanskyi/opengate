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

type filingFixture struct {
	devices  device.Repository
	ctx      context.Context
	contoso  uuid.UUID
	fabrikam uuid.UUID
	dallas   *device.Site
	device   *device.Device
}

func newFilingFixture(t *testing.T) filingFixture {
	t.Helper()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	contoso := newCustomer(t, ctx, store, "Contoso")
	return filingFixture{
		devices:  devices,
		ctx:      ctx,
		contoso:  contoso,
		fabrikam: newCustomer(t, ctx, store, "Fabrikam"),
		dallas:   newSite(t, ctx, store, contoso, "Dallas"),
		device:   testutil.SeedDevice(t, ctx, store, uuid.Nil),
	}
}

func (f filingFixture) inContosoDallas(t *testing.T) {
	t.Helper()
	require.NoError(t, f.devices.UpdateOrganization(f.ctx, f.device.ID, f.contoso))
	require.NoError(t, f.devices.UpdateSite(f.ctx, f.device.ID, f.dallas.ID))
}

func (f filingFixture) read(t *testing.T) *device.Device {
	t.Helper()
	got, err := f.devices.Get(f.ctx, f.device.ID)
	require.NoError(t, err)
	return got
}

func TestDeviceSiteMustBeInTheDeviceOrganization(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	require.NoError(t, f.devices.UpdateOrganization(f.ctx, f.device.ID, f.fabrikam))

	err := f.devices.UpdateSite(f.ctx, f.device.ID, f.dallas.ID)
	require.ErrorIs(t, err, device.ErrSiteNotInOrganization)
	assert.Equal(t, uuid.Nil, f.read(t).SiteID, "a refused move leaves the machine where it was")
}

func TestDeviceTakesASiteInItsOwnOrganization(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	f.inContosoDallas(t)

	assert.Equal(t, f.dallas.ID, f.read(t).SiteID)
}

func TestMovingACustomerClearsTheSite(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	f.inContosoDallas(t)

	require.NoError(t, f.devices.UpdateOrganization(f.ctx, f.device.ID, f.fabrikam))

	got := f.read(t)
	assert.Equal(t, f.fabrikam, got.OrganizationID)
	assert.Equal(t, uuid.Nil, got.SiteID, "the old customer's office does not travel with the machine")
}

func TestAReconnectAfterAMoveDoesNotResurrectTheOldSite(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	f.inContosoDallas(t)
	require.NoError(t, f.devices.UpdateOrganization(f.ctx, f.device.ID, f.fabrikam))

	reconnect := &device.Device{
		ID: f.device.ID, SiteID: f.dallas.ID, Hostname: f.device.Hostname, Status: device.StatusOnline,
	}
	require.NoError(t, f.devices.Upsert(f.ctx, reconnect), "the agent must be able to come back")

	got := f.read(t)
	assert.Equal(t, f.fabrikam, got.OrganizationID)
	assert.Equal(t, uuid.Nil, got.SiteID, "the office it remembers is not one its customer has")
	assert.Equal(t, device.StatusOnline, got.Status)
}

func TestRegistrationIgnoresASiteOutsideTheDeviceCustomer(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)

	// No customer named, so the machine lands in the tenant's own organization.
	fresh := &device.Device{
		ID: uuid.New(), SiteID: f.dallas.ID, Hostname: "new-agent", Status: device.StatusOnline,
	}
	require.NoError(t, f.devices.Upsert(f.ctx, fresh))

	got, err := f.devices.Get(f.ctx, fresh.ID)
	require.NoError(t, err)
	assert.NotEqual(t, f.contoso, got.OrganizationID)
	assert.Equal(t, uuid.Nil, got.SiteID, "a site outside the machine's customer is dropped, not honoured")
}
