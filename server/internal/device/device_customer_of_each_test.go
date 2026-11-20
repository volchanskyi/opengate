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

func TestEachMachineOfTheTenantIsListedWithItsCustomer(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)

	tenant := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenant, "Listed "+tenant.String()[:8])
	ctx := dbtx.WithTenant(context.Background(), tenant, false)
	contoso := newCustomer(t, ctx, store, "Contoso")
	fabrikam := newCustomer(t, ctx, store, "Fabrikam")
	dallas := testutil.SeedDeviceIn(t, ctx, store, contoso, uuid.Nil)
	austin := testutil.SeedDeviceIn(t, ctx, store, contoso, uuid.Nil)
	seattle := testutil.SeedDeviceIn(t, ctx, store, fabrikam, uuid.Nil)

	northwind := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, northwind, "Other "+northwind.String()[:8])
	otherCtx := dbtx.WithTenant(context.Background(), northwind, false)
	testutil.SeedDeviceIn(t, otherCtx, store, newCustomer(t, otherCtx, store, "Northwind"), uuid.Nil)

	listed, err := devices.CustomerOfEach(ctx)
	require.NoError(t, err)

	assert.Equal(t, map[device.DeviceID]device.OrganizationID{
		dallas.ID: contoso, austin.ID: contoso, seattle.ID: fabrikam,
	}, listed, "the tenant's own machines and no other tenant's")
}
