package device_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newSite creates a site under organizationID and returns it.
func newSite(t *testing.T, ctx context.Context, store *db.PostgresStore, organizationID uuid.UUID, name string) *device.Site {
	t.Helper()
	s := &device.Site{ID: uuid.New(), OrganizationID: organizationID, Name: name + "-" + uuid.New().String()[:8]}
	require.NoError(t, testutil.NewTestSites(t, store).Create(ctx, s))
	return s
}

// TestSiteBelongsToExactlyOneOrganization is the structural half of the level:
// a site names one customer and is listed under that customer alone, so the
// Dallas office cannot show up while a technician is looking at Fabrikam.
func TestSiteBelongsToExactlyOneOrganization(t *testing.T) {
	t.Parallel()
	_, sites, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	contoso := newCustomer(t, ctx, store, "Contoso")
	fabrikam := newCustomer(t, ctx, store, "Fabrikam")
	dallas := newSite(t, ctx, store, contoso, "Dallas")

	underContoso, err := sites.List(ctx, contoso)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{dallas.ID}, siteIDs(underContoso), "Dallas is listed under the customer that owns it")

	underFabrikam, err := sites.List(ctx, fabrikam)
	require.NoError(t, err)
	assert.NotContains(t, siteIDs(underFabrikam), dallas.ID, "another customer's site is never listed")

	wholeTenant, err := sites.List(ctx, uuid.Nil)
	require.NoError(t, err)
	assert.Contains(t, siteIDs(wholeTenant), dallas.ID, "no customer named returns the whole tenant")
}

// TestSiteCannotBeCreatedInAnotherTenantsCustomer closes the same hole a device
// move closes: a foreign-key check runs past row-level security, so the
// constraint alone would accept a customer id belonging to somebody else.
func TestSiteCannotBeCreatedInAnotherTenantsCustomer(t *testing.T) {
	t.Parallel()
	_, sites, _, store := newRepos(t)
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)

	tenantB := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	foreign := newCustomer(t, ctxB, store, "Elsewhere")

	err := sites.Create(ctxA, &device.Site{ID: uuid.New(), OrganizationID: foreign, Name: "Smuggled"})
	require.ErrorIs(t, err, device.ErrOrganizationNotFound)
}

// TestDeletingASiteLeavesItsDevicesInTheCustomer proves the narrower level can
// be removed without taking machines with it: closing the Dallas office leaves
// its twelve machines in Contoso, unfiled, rather than deleting them.
func TestDeletingASiteLeavesItsDevicesInTheCustomer(t *testing.T) {
	t.Parallel()
	devices, sites, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	contoso := newCustomer(t, ctx, store, "Contoso")
	dallas := newSite(t, ctx, store, contoso, "Dallas")

	d := testutil.SeedDevice(t, ctx, store, uuid.Nil)
	require.NoError(t, devices.UpdateOrganization(ctx, d.ID, contoso))
	require.NoError(t, devices.UpdateSite(ctx, d.ID, dallas.ID))

	require.NoError(t, sites.Delete(ctx, dallas.ID))

	got, err := devices.Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, contoso, got.OrganizationID, "the machine stays with its customer")
	assert.Equal(t, uuid.Nil, got.SiteID, "and is simply unfiled")
}

// TestDeletingACustomerTakesItsSites is the other direction: a customer leaving
// takes its offices with it, so nothing is left pointing at a customer that no
// longer exists.
func TestDeletingACustomerTakesItsSites(t *testing.T) {
	t.Parallel()
	_, sites, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	keep := newCustomer(t, ctx, store, "Contoso")
	leaving := newCustomer(t, ctx, store, "Adventure Works")
	kept := newSite(t, ctx, store, keep, "Dallas")
	going := newSite(t, ctx, store, leaving, "Head Office")

	require.NoError(t, testutil.NewTestOrganizations(t, store).Delete(ctx, leaving))

	remaining, err := sites.List(ctx, uuid.Nil)
	require.NoError(t, err)
	assert.Contains(t, siteIDs(remaining), kept.ID)
	assert.NotContains(t, siteIDs(remaining), going.ID, "a departed customer leaves no offices behind")
}

// TestTwoCustomersMayShareASiteName covers the name collision the tenancy model
// has to tolerate: "Head Office" means a different building for each customer,
// so uniqueness is per customer rather than per tenant.
func TestTwoCustomersMayShareASiteName(t *testing.T) {
	t.Parallel()
	_, sites, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	contoso := newCustomer(t, ctx, store, "Contoso")
	fabrikam := newCustomer(t, ctx, store, "Fabrikam")

	require.NoError(t, sites.Create(ctx, &device.Site{ID: uuid.New(), OrganizationID: contoso, Name: "Head Office"}))
	require.NoError(t, sites.Create(ctx, &device.Site{ID: uuid.New(), OrganizationID: fabrikam, Name: "Head Office"}))

	dup := sites.Create(ctx, &device.Site{ID: uuid.New(), OrganizationID: contoso, Name: "Head Office"})
	require.ErrorIs(t, dup, device.ErrSiteNameTaken, "the same customer cannot have the office twice")
}

func siteIDs(sites []*device.Site) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(sites))
	for _, s := range sites {
		ids = append(ids, s.ID)
	}
	return ids
}
