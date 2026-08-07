package settings_test

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
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func newOrganization(id uuid.UUID, name string) *organization.Organization {
	return &organization.Organization{ID: id, Name: name}
}

// fileDevice seeds a machine into organizationID and files it into siteID,
// returning its id.
func fileDevice(t *testing.T, ctx context.Context, store *db.PostgresStore, devices device.Repository, organizationID, siteID uuid.UUID) uuid.UUID {
	t.Helper()
	d := testutil.SeedDevice(t, ctx, store, uuid.Nil)
	require.NoError(t, devices.UpdateOrganization(ctx, d.ID, organizationID))
	require.NoError(t, devices.UpdateSite(ctx, d.ID, siteID))
	return d.ID
}

// TestTheDiskRuleResolvesDownTheRealLadder is the worked case N5 names, end to
// end against real rows rather than a hand-built scope: Contoso's Dallas office
// is all file servers and alarms at 95, the one workstation in it alarms at 90,
// and a machine in Contoso's Austin office — which sets nothing — takes
// Contoso's own number.
func TestTheDiskRuleResolvesDownTheRealLadder(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	devices := testutil.NewTestDevices(t, store)
	sites := testutil.NewTestSites(t, store)
	reader := settings.NewPostgresReader(store.DB())

	contoso := uuid.New()
	require.NoError(t, testutil.NewTestOrganizations(t, store).Create(ctx, newOrganization(contoso, "Contoso")))

	dallas := &device.Site{ID: uuid.New(), OrganizationID: contoso, Name: "Dallas"}
	austin := &device.Site{ID: uuid.New(), OrganizationID: contoso, Name: "Austin"}
	require.NoError(t, sites.Create(ctx, dallas))
	require.NoError(t, sites.Create(ctx, austin))

	fileServer := fileDevice(t, ctx, store, devices, contoso, dallas.ID)
	workstation := fileDevice(t, ctx, store, devices, contoso, dallas.ID)
	elsewhere := fileDevice(t, ctx, store, devices, contoso, austin.ID)

	// The two numbers the plan names, plus the rungs above them.
	const shippedThreshold = 90
	overrides := []settings.Override[int]{
		{Level: settings.LevelSite, ScopeID: dallas.ID, Value: 95},
		{Level: settings.LevelDevice, ScopeID: workstation, Value: 90},
		{Level: settings.LevelOrganization, ScopeID: contoso, Value: 85},
		{Level: settings.LevelTenant, ScopeID: dbtx.DefaultTenantID, Value: 80},
	}

	cases := []struct {
		name      string
		deviceID  uuid.UUID
		want      int
		wantLevel settings.Level
	}{
		{"a Dallas file server takes the office's 95", fileServer, 95, settings.LevelSite},
		{"the one workstation in Dallas keeps its own 90", workstation, 90, settings.LevelDevice},
		{"a machine in Austin falls through to Contoso's 85", elsewhere, 85, settings.LevelOrganization},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := reader.ScopeFor(ctx, tc.deviceID)
			require.NoError(t, err)

			got, level := settings.Resolve(scope, overrides, shippedThreshold, settings.NarrowestWins)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLevel, level)
		})
	}
}

// TestScopeForAnUnfiledMachineHasNoSiteRung covers the machine nobody has filed:
// the ladder still resolves, it simply has one rung fewer.
func TestScopeForAnUnfiledMachineHasNoSiteRung(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	d := testutil.SeedDevice(t, ctx, store, uuid.Nil)
	scope, err := settings.NewPostgresReader(store.DB()).ScopeFor(ctx, d.ID)
	require.NoError(t, err)

	assert.Equal(t, d.ID, scope.DeviceID)
	assert.Equal(t, uuid.Nil, scope.SiteID, "an unfiled machine has no site rung")
	assert.NotEqual(t, uuid.Nil, scope.OrganizationID, "but always has a customer")
	assert.Equal(t, dbtx.DefaultTenantID, scope.TenantID)

	_, present := scope.Key(settings.LevelSite)
	assert.False(t, present)
}

// TestScopeForAnotherTenantsDeviceIsNotFound keeps the ladder inside the wall:
// a device in another tenant reads the same as one that does not exist.
func TestScopeForAnotherTenantsDeviceIsNotFound(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)

	tenantB := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)

	d := testutil.SeedDevice(t, ctxA, store, uuid.Nil)

	_, err := settings.NewPostgresReader(store.DB()).ScopeFor(ctxB, d.ID)
	assert.ErrorIs(t, err, settings.ErrDeviceNotFound)

	_, err = settings.NewPostgresReader(store.DB()).ScopeFor(ctxA, uuid.New())
	assert.ErrorIs(t, err, settings.ErrDeviceNotFound)
}
