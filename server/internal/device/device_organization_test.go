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
	"github.com/volchanskyi/opengate/server/internal/organization"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newCustomer creates a customer in ctx's tenant and returns its id.
func newCustomer(t *testing.T, ctx context.Context, store *db.PostgresStore, name string) uuid.UUID {
	t.Helper()
	org := &organization.Organization{ID: uuid.New(), Name: name + "-" + uuid.New().String()[:8]}
	require.NoError(t, testutil.NewTestOrganizations(t, store).Create(ctx, org))
	return org.ID
}

// TestDeviceAlwaysLandsInAnOrganization is the no-orphan rule at the point a
// device row is written: the agent connection path names no customer, so the row
// takes the tenant's own rather than being refused or left dangling.
func TestDeviceAlwaysLandsInAnOrganization(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	d := &device.Device{ID: uuid.New(), SiteID: site.ID, Hostname: "unassigned", Status: device.StatusOffline}
	require.NoError(t, devices.Upsert(ctx, d))

	got, err := devices.Get(ctx, d.ID)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, got.OrganizationID, "a device with no customer named still belongs to one")

	fallback, err := testutil.NewTestOrganizations(t, store).EnsureDefault(ctx)
	require.NoError(t, err)
	assert.Equal(t, fallback, got.OrganizationID, "the fallback is the tenant's own organization")
}

// TestUpsertKeepsTheDeviceOrganizationOnReconnect covers the agent reconnecting
// after a move: a re-registration names no customer, and must not drag the
// device back to the tenant's default.
func TestUpsertKeepsTheDeviceOrganizationOnReconnect(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	d := testutil.SeedDevice(t, ctx, store, site.ID)
	contoso := newCustomer(t, ctx, store, "Contoso")
	require.NoError(t, devices.UpdateOrganization(ctx, d.ID, contoso))

	reconnect := &device.Device{ID: d.ID, SiteID: site.ID, Hostname: d.Hostname, Status: device.StatusOnline}
	require.NoError(t, devices.Upsert(ctx, reconnect))

	got, err := devices.Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, contoso, got.OrganizationID, "a reconnect must not undo a move")
	assert.Equal(t, device.StatusOnline, got.Status)
}
