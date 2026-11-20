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
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// returning is a machine the server has seen before, in Contoso's Dallas site, in maintenance and
// tagged as a file server.
type returning struct {
	store   *db.PostgresStore
	ctx     context.Context
	tenant  uuid.UUID
	contoso uuid.UUID
	dallas  uuid.UUID
	machine *device.Device
}

func newReturning(t *testing.T) returning {
	t.Helper()
	_, _, _, store := newRepos(t)
	tenantID := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenantID, "Returning "+tenantID.String()[:8])
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)

	contoso := newCustomer(t, ctx, store, "Contoso")
	dallas := newSite(t, ctx, store, contoso, "Dallas")
	machine := testutil.SeedDeviceIn(t, ctx, store, contoso, dallas.ID)
	technician := testutil.SeedUser(t, ctx, store)
	require.NoError(t, testutil.NewTestDevices(t, store).SetMaintenance(ctx, machine.ID, true, technician.ID, "patching"))

	tags := rules.NewStore(store.DB())
	label := rules.Label{ID: uuid.New(), OrganizationID: contoso, Key: "role", Value: "file-server", CreatedBy: "ivan"}
	require.NoError(t, tags.CreateLabel(ctx, label))
	require.NoError(t, tags.AssignTag(ctx, machine.ID, label.ID, "ivan"))

	return returning{store: store, ctx: ctx, tenant: tenantID, contoso: contoso, dallas: dallas.ID, machine: machine}
}

func (r returning) signIn(t *testing.T, repo device.Repository, needsFleetSize func(uuid.UUID) bool) device.SignIn {
	t.Helper()
	got, err := repo.SignIn(dbtx.WithDefaultTenant(context.Background(), false), &device.Device{
		ID: r.machine.ID, Hostname: "dallas-fs01", OS: "linux", Status: device.StatusOnline,
	}, needsFleetSize)
	require.NoError(t, err)
	return got
}

func never(uuid.UUID) bool  { return false }
func always(uuid.UUID) bool { return true }

func TestASignInReadsBackTheMachinesPlaceFlagAndTags(t *testing.T) {
	t.Parallel()
	r := newReturning(t)

	got := r.signIn(t, testutil.NewTestDevices(t, r.store), never)

	assert.Equal(t, device.SignIn{
		TenantID: r.tenant, OrganizationID: r.contoso, SiteID: r.dallas, MaintenanceOn: true,
		Tags: map[string]string{"role": "file-server"},
	}, got)
	written, err := testutil.NewTestDevices(t, r.store).Get(r.ctx, r.machine.ID)
	require.NoError(t, err)
	assert.Equal(t, device.StatusOnline, written.Status)
	assert.Equal(t, "dallas-fs01", written.Hostname)
}

func TestASignInCountsTheCustomersMachinesOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	r := newReturning(t)
	testutil.SeedDeviceIn(t, r.ctx, r.store, r.contoso, uuid.Nil)
	testutil.SeedDeviceIn(t, r.ctx, r.store, newCustomer(t, r.ctx, r.store, "Fabrikam"), uuid.Nil)
	repo := testutil.NewTestDevices(t, r.store)

	assert.Zero(t, r.signIn(t, repo, never).FleetSize)
	assert.Equal(t, 2, r.signIn(t, repo, always).FleetSize, "Contoso's two machines, and not Fabrikam's")
	var asked uuid.UUID
	r.signIn(t, repo, func(org uuid.UUID) bool { asked = org; return false })
	assert.Equal(t, r.contoso, asked, "the question names the machine's own customer")
}

func TestAFirstSignInLandsInTheDefaultTenantsDefaultCustomer(t *testing.T) {
	t.Parallel()
	_, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	fallback, err := testutil.NewTestOrganizations(t, store).EnsureDefault(ctx)
	require.NoError(t, err)

	got, err := testutil.NewTestDevices(t, store).SignIn(dbtx.WithDefaultTenant(context.Background(), false), &device.Device{
		ID: uuid.New(), Hostname: "new-machine", Status: device.StatusOnline,
	}, always)

	require.NoError(t, err)
	assert.Equal(t, device.SignIn{TenantID: dbtx.DefaultTenantID, OrganizationID: fallback, Tags: map[string]string{},
		FleetSize: 1}, got)
}

func TestASignInWritesOnlyItsOwnTenantsMachine(t *testing.T) {
	t.Parallel()
	r := newReturning(t)
	devices := testutil.NewTestDevices(t, r.store)
	elsewhere := dbtx.WithDefaultTenant(context.Background(), false)
	other := testutil.SeedDevice(t, elsewhere, r.store, uuid.Nil)

	got := r.signIn(t, devices, always)

	assert.Equal(t, r.tenant, got.TenantID)
	assert.Equal(t, 1, got.FleetSize, "the count stays inside the machine's tenant")
	untouched, err := devices.Get(elsewhere, other.ID)
	require.NoError(t, err)
	assert.Equal(t, other.Hostname, untouched.Hostname)
	assert.Equal(t, other.Status, untouched.Status, "another tenant's machine is left as it was")
}

func TestASignInIsOneTransactionOfFiveTrips(t *testing.T) {
	t.Parallel()
	r := newReturning(t)
	counter := &testutil.StatementCounter{}
	traced := device.NewPostgresDevices(testutil.NewTracedDB(t, r.store, counter))

	r.signIn(t, traced, never)
	trips := counter.Take()
	require.Len(t, trips, 5, "begin, the scope, the tenant, the write with its tags, commit: %q", trips)
	assert.Equal(t, "begin", trips[0])
	assert.Equal(t, "commit", trips[len(trips)-1])

	r.signIn(t, traced, always)
	assert.Len(t, counter.Take(), 6, "a staged rollout adds the customer's count")
}

func TestASignInTheDatabaseCannotAnswerFails(t *testing.T) {
	t.Parallel()
	repo := device.NewPostgresDevices(testutil.NewUnmigratedDB(t))

	_, err := repo.SignIn(dbtx.WithDefaultTenant(context.Background(), false), &device.Device{ID: uuid.New(), Status: device.StatusOnline}, never)

	assert.Error(t, err)
}
