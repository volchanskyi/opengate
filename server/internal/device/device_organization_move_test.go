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

// TestMoveDeviceBetweenOrganizations proves a move is complete: the device
// answers under its new customer, no longer under the old one, and everything
// keyed to the device — hardware, site, status — comes with it rather than
// being left behind or rewritten.
func TestMoveDeviceBetweenOrganizations(t *testing.T) {
	t.Parallel()
	devices, _, hardware, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	d := testutil.SeedDevice(t, ctx, store, site.ID)
	require.NoError(t, hardware.Upsert(ctx, &device.Hardware{DeviceID: d.ID, CPUModel: "Ryzen 9 7950X"}))

	from := newCustomer(t, ctx, store, "Fabrikam")
	to := newCustomer(t, ctx, store, "Contoso")
	require.NoError(t, devices.UpdateOrganization(ctx, d.ID, from))
	require.NoError(t, devices.UpdateOrganization(ctx, d.ID, to))

	inNew, err := devices.List(ctx, device.Filter{OrganizationID: to})
	require.NoError(t, err)
	require.Len(t, inNew, 1)
	assert.Equal(t, d.ID, inNew[0].ID)

	inOld, err := devices.List(ctx, device.Filter{OrganizationID: from})
	require.NoError(t, err)
	assert.Empty(t, inOld, "the device must not answer under the customer it left")

	// The rows that hang off a device are keyed by device id, so a move carries
	// them without a rewrite. Reading them back after the move is what proves it.
	hw, err := hardware.Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ryzen 9 7950X", hw.CPUModel, "hardware history follows the device")

	moved, err := devices.Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, moved.SiteID, "the office the machine left does not travel with it")
	assert.Equal(t, d.Hostname, moved.Hostname)
}

// TestMoveRefusesAnOrganizationOutsideTheTenant closes the hole a foreign key
// alone would leave open: constraint checks run past row-level security, so
// without an explicit scope check a device could be pushed into another tenant's
// customer.
func TestMoveRefusesAnOrganizationOutsideTheTenant(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)

	tenantB := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)

	site := testutil.SeedSite(t, ctxA, store)
	d := testutil.SeedDevice(t, ctxA, store, site.ID)
	before, err := devices.Get(ctxA, d.ID)
	require.NoError(t, err)

	foreign := newCustomer(t, ctxB, store, "Elsewhere")
	assert.ErrorIs(t, devices.UpdateOrganization(ctxA, d.ID, foreign), device.ErrOrganizationNotFound)

	after, err := devices.Get(ctxA, d.ID)
	require.NoError(t, err)
	assert.Equal(t, before.OrganizationID, after.OrganizationID, "the refused move must change nothing")
}

// TestMoveMissingDeviceAndMissingOrganization covers the two not-found halves.
func TestMoveMissingDeviceAndMissingOrganization(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	site := testutil.SeedSite(t, ctx, store)
	d := testutil.SeedDevice(t, ctx, store, site.ID)
	customer := newCustomer(t, ctx, store, "Contoso")

	assert.ErrorIs(t, devices.UpdateOrganization(ctx, uuid.New(), customer), device.ErrDeviceNotFound)
	assert.ErrorIs(t, devices.UpdateOrganization(ctx, d.ID, uuid.New()), device.ErrOrganizationNotFound)
}
