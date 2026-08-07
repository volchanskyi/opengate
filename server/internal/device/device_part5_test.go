package device_test

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
	"time"
)

func TestPostgresDeviceRepos_TenantDeny(t *testing.T) {
	t.Parallel()
	devices, sites, hardware, store := newRepos(t)
	tenantB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	groupA := testutil.SeedSite(t, ctxA, store)
	groupB := testutil.SeedSite(t, ctxB, store)
	deviceA := testutil.SeedDevice(t, ctxA, store, groupA.ID)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)
	require.NoError(t, hardware.Upsert(ctxA, &device.Hardware{DeviceID: deviceA.ID, CPUModel: "tenant-a"}))
	require.NoError(t, hardware.Upsert(ctxB, &device.Hardware{DeviceID: deviceB.ID, CPUModel: "tenant-b"}))

	_, err := devices.Get(ctxA, deviceB.ID)
	assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	_, err = sites.Get(ctxA, groupB.ID)
	assert.ErrorIs(t, err, device.ErrSiteNotFound)
	_, err = hardware.Get(ctxA, deviceB.ID)
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)

	allDevices, err := devices.List(ctxA, device.Filter{})
	require.NoError(t, err)
	assert.Len(t, allDevices, 1)
	assert.Equal(t, deviceA.ID, allDevices[0].ID)

	devicesInBGroup, err := devices.List(ctxA, device.Filter{SiteID: groupB.ID})
	require.NoError(t, err)
	assert.Empty(t, devicesInBGroup)
	sitesInA, err := sites.List(ctxA, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, sitesInA, 1, "tenant A sees its own site and nothing of tenant B's")
	assert.Equal(t, groupA.ID, sitesInA[0].ID)

	countsA, err := devices.Counts(ctxA, uuid.Nil)
	require.NoError(t, err)
	assert.Equal(t, 1, countsA.Total, "the rollup counts tenant A's device only")

	resolvedTenant, err := devices.TenantForDevice(dbtx.WithDefaultTenant(context.Background(), true), deviceB.ID)
	require.NoError(t, err)
	assert.Equal(t, tenantB, resolvedTenant)
	_, err = devices.TenantForDevice(ctxA, deviceB.ID)
	assert.ErrorIs(t, err, device.ErrDeviceNotFound)

	_, err = devices.List(context.Background(), device.Filter{})
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = sites.Get(context.Background(), groupA.ID)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = hardware.Get(context.Background(), deviceA.ID)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}

// fakeObserver records every Observe call for the Instrumented decorator tests.
type fakeObserver struct {
	calls []observerCall
}

type observerCall struct {
	op       string
	duration time.Duration
	ok       bool
}

func (f *fakeObserver) Observe(op string, d time.Duration, ok bool) {
	f.calls = append(f.calls, observerCall{op: op, duration: d, ok: ok})
}

type memDevices struct {
	failEvery bool
}

func (m *memDevices) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}

func (m *memDevices) Upsert(_ context.Context, _ *device.Device) error { return m.maybeFail() }

func (m *memDevices) Get(_ context.Context, _ device.DeviceID) (*device.Device, error) {
	return &device.Device{}, m.maybeFail()
}

func (m *memDevices) TenantForDevice(_ context.Context, _ device.DeviceID) (uuid.UUID, error) {
	return uuid.Nil, m.maybeFail()
}

func (m *memDevices) List(_ context.Context, _ device.Filter) ([]*device.Device, error) {
	return nil, m.maybeFail()
}

func (m *memDevices) UpdateOrganization(_ context.Context, _ device.DeviceID, _ device.OrganizationID) error {
	return m.maybeFail()
}

func (m *memDevices) ListForOwner(_ context.Context, _ uuid.UUID) ([]*device.Device, error) {
	return nil, m.maybeFail()
}
