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

// filingFixture is the estate every filing case works against: two customers in
// one tenant, an office belonging to the first, and one machine.
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

// inContosoDallas puts the fixture's machine in Contoso and files it into the
// Dallas office, which is the starting state for the cases about leaving it.
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

// TestDeviceSiteMustBeInTheDeviceOrganization is the mismatch the plan refuses
// to accept silently: the machine belongs to Fabrikam, so filing it into
// Contoso's Dallas office is an error, not a quietly stored wrong answer.
func TestDeviceSiteMustBeInTheDeviceOrganization(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	require.NoError(t, f.devices.UpdateOrganization(f.ctx, f.device.ID, f.fabrikam))

	err := f.devices.UpdateSite(f.ctx, f.device.ID, f.dallas.ID)
	require.ErrorIs(t, err, device.ErrSiteNotInOrganization)
	assert.Equal(t, uuid.Nil, f.read(t).SiteID, "a refused move leaves the machine where it was")
}

// TestDeviceTakesASiteInItsOwnOrganization is the positive case of the rule
// above.
func TestDeviceTakesASiteInItsOwnOrganization(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	f.inContosoDallas(t)

	assert.Equal(t, f.dallas.ID, f.read(t).SiteID)
}

// TestMovingACustomerClearsTheSite is the leak the option run surfaced: a laptop
// that follows its owner from Contoso to Fabrikam must not arrive still filed
// into a Contoso office. The site is the narrower level, so it cannot survive
// the level above it changing.
func TestMovingACustomerClearsTheSite(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)
	f.inContosoDallas(t)

	require.NoError(t, f.devices.UpdateOrganization(f.ctx, f.device.ID, f.fabrikam))

	got := f.read(t)
	assert.Equal(t, f.fabrikam, got.OrganizationID)
	assert.Equal(t, uuid.Nil, got.SiteID, "the old customer's office does not travel with the machine")
}

// TestAReconnectAfterAMoveDoesNotResurrectTheOldSite is the case that would
// otherwise lock a machine out: the agent still believes it is in the office it
// was enrolled into and re-sends that site on every reconnect. After a move that
// office belongs to another customer, so honouring the agent would fail the pair
// constraint and refuse the registration outright. Filing is a server-side
// decision, so the stored answer stands.
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

// TestRegistrationIgnoresASiteOutsideTheDeviceCustomer covers the same rule at
// first registration, where there is no stored answer to fall back on: the
// machine lands unfiled rather than being refused.
func TestRegistrationIgnoresASiteOutsideTheDeviceCustomer(t *testing.T) {
	t.Parallel()
	f := newFilingFixture(t)

	// No customer named, so the machine lands in the tenant's own, not Contoso.
	fresh := &device.Device{
		ID: uuid.New(), SiteID: f.dallas.ID, Hostname: "new-agent", Status: device.StatusOnline,
	}
	require.NoError(t, f.devices.Upsert(f.ctx, fresh))

	got, err := f.devices.Get(f.ctx, fresh.ID)
	require.NoError(t, err)
	assert.NotEqual(t, f.contoso, got.OrganizationID)
	assert.Equal(t, uuid.Nil, got.SiteID, "a site outside the machine's customer is dropped, not honoured")
}
