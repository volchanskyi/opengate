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
	devices, groups, hardware, store := newRepos(t)
	orgB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), orgB, false)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	ownerA := testutil.SeedUser(t, ctxA, store)
	ownerB := testutil.SeedUser(t, ctxB, store)
	groupA := testutil.SeedGroup(t, ctxA, store, ownerA.ID)
	groupB := testutil.SeedGroup(t, ctxB, store, ownerB.ID)
	deviceA := testutil.SeedDevice(t, ctxA, store, groupA.ID)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)
	require.NoError(t, hardware.Upsert(ctxA, &device.Hardware{DeviceID: deviceA.ID, CPUModel: "tenant-a"}))
	require.NoError(t, hardware.Upsert(ctxB, &device.Hardware{DeviceID: deviceB.ID, CPUModel: "tenant-b"}))

	_, err := devices.Get(ctxA, deviceB.ID)
	assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	_, err = groups.Get(ctxA, groupB.ID)
	assert.ErrorIs(t, err, device.ErrGroupNotFound)
	_, err = hardware.Get(ctxA, deviceB.ID)
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)

	allDevices, err := devices.ListAll(ctxA)
	require.NoError(t, err)
	assert.Len(t, allDevices, 1)
	assert.Equal(t, deviceA.ID, allDevices[0].ID)

	devicesInBGroup, err := devices.List(ctxA, groupB.ID)
	require.NoError(t, err)
	assert.Empty(t, devicesInBGroup)
	devicesForBOwner, err := devices.ListForOwner(ctxA, ownerB.ID)
	require.NoError(t, err)
	assert.Empty(t, devicesForBOwner)
	groupsForBOwner, err := groups.List(ctxA, ownerB.ID)
	require.NoError(t, err)
	assert.Empty(t, groupsForBOwner)

	resolvedOrg, err := devices.OrgForDevice(dbtx.WithDefaultTenant(context.Background(), true), deviceB.ID)
	require.NoError(t, err)
	assert.Equal(t, orgB, resolvedOrg)
	_, err = devices.OrgForDevice(ctxA, deviceB.ID)
	assert.ErrorIs(t, err, device.ErrDeviceNotFound)

	_, err = devices.ListAll(context.Background())
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = groups.Get(context.Background(), groupA.ID)
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

func (m *memDevices) OrgForDevice(_ context.Context, _ device.DeviceID) (uuid.UUID, error) {
	return uuid.Nil, m.maybeFail()
}

func (m *memDevices) List(_ context.Context, _ device.GroupID) ([]*device.Device, error) {
	return nil, m.maybeFail()
}

func (m *memDevices) ListAll(_ context.Context) ([]*device.Device, error) { return nil, m.maybeFail() }

func (m *memDevices) ListForOwner(_ context.Context, _ uuid.UUID) ([]*device.Device, error) {
	return nil, m.maybeFail()
}
