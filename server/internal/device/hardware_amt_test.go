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

type amtFixture struct {
	hardware device.HardwareRepository
	store    *db.PostgresStore
	ctx      context.Context
	deviceID device.DeviceID
	tenantID uuid.UUID
}

func (f amtFixture) seedSibling(t *testing.T) device.DeviceID {
	t.Helper()
	site := testutil.SeedSite(t, f.ctx, f.store)
	return testutil.SeedDevice(t, f.ctx, f.store, site.ID).ID
}

func newAMTFixture(t *testing.T, tenantID uuid.UUID) amtFixture {
	t.Helper()
	_, _, hardware, store := newRepos(t)

	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	if tenantID != uuid.Nil {
		testutil.EnsureTenant(t, context.Background(), store, tenantID, "Tenant "+tenantID.String()[:8])
		ctx = dbtx.WithTenant(context.Background(), tenantID, false)
	} else {
		tenantID = dbtx.DefaultTenantID
	}

	f := amtFixture{hardware: hardware, store: store, ctx: ctx, tenantID: tenantID}
	f.deviceID = f.seedSibling(t)
	return f
}

func report(deviceID uuid.UUID, systemUUID *uuid.UUID, cpu string) *device.Hardware {
	available := true
	return &device.Hardware{
		DeviceID:     deviceID,
		CPUModel:     cpu,
		CPUCores:     8,
		SystemUUID:   systemUUID,
		AMTAvailable: &available,
		AMTVersion:   "16.1.30.2260",
	}
}

func TestHardwareAMTColumnsSurviveBothWriters(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)
	systemUUID := uuid.New()

	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i7-12700K")))
	require.NoError(t, f.hardware.SetAMTDetail(f.ctx, f.deviceID, "OptiPlex 7090", "16.1.25"))

	hw, err := f.hardware.Get(f.ctx, f.deviceID)
	require.NoError(t, err)
	assert.Equal(t, "Intel Core i7-12700K", hw.CPUModel)
	assert.Equal(t, 8, hw.CPUCores)
	require.NotNil(t, hw.AMTAvailable)
	assert.True(t, *hw.AMTAvailable)
	assert.Equal(t, "16.1.30.2260", hw.AMTVersion)
	assert.Equal(t, "OptiPlex 7090", hw.AMTModel)
	assert.Equal(t, "16.1.25", hw.AMTFirmware)

	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i9-13900K")))

	hw, err = f.hardware.Get(f.ctx, f.deviceID)
	require.NoError(t, err)
	assert.Equal(t, "Intel Core i9-13900K", hw.CPUModel)
	assert.Equal(t, "OptiPlex 7090", hw.AMTModel, "an agent report must not blank the WSMAN-sourced model")
	assert.Equal(t, "16.1.25", hw.AMTFirmware, "an agent report must not blank the WSMAN-sourced firmware")
}

func TestHardwareUpsertAMTPresenceSkew(t *testing.T) {
	t.Parallel()
	absent := false

	tests := []struct {
		name          string
		second        *device.Hardware
		wantAvailable bool
		wantVersion   string
	}{
		{
			name:          "a silent agent preserves the known capability",
			second:        &device.Hardware{CPUModel: "Intel Core i7-12700K"},
			wantAvailable: true,
			wantVersion:   "16.1.30.2260",
		},
		{
			name:          "a stated absence overwrites the known capability",
			second:        &device.Hardware{CPUModel: "AMD Ryzen 9 7950X", AMTAvailable: &absent},
			wantAvailable: false,
			wantVersion:   "16.1.30.2260",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAMTFixture(t, uuid.Nil)
			systemUUID := uuid.New()
			require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i7-12700K")))

			tt.second.DeviceID = f.deviceID
			require.NoError(t, f.hardware.Upsert(f.ctx, tt.second))

			hw, err := f.hardware.Get(f.ctx, f.deviceID)
			require.NoError(t, err)
			require.NotNil(t, hw.AMTAvailable)
			assert.Equal(t, tt.wantAvailable, *hw.AMTAvailable)
			assert.Equal(t, tt.wantVersion, hw.AMTVersion)

			// The join key survives either way, which keeps the AMT connection linked.
			gotDevice, _, err := f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
			require.NoError(t, err)
			assert.Equal(t, f.deviceID, gotDevice)
		})
	}
}

func TestResolveBySystemUUIDCrossesTenants(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.New())
	systemUUID := uuid.New()
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i5-1145G7")))

	// The context carries no tenant, as on an MPS connection.
	gotDevice, gotTenant, err := f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
	require.NoError(t, err)
	assert.Equal(t, f.deviceID, gotDevice)
	assert.Equal(t, f.tenantID, gotTenant, "the resolved tenant is what scopes every later write")
}

func TestResolveBySystemUUIDUnknownKey(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)

	_, _, err := f.hardware.ResolveBySystemUUID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)
}

func TestResolveBySystemUUIDAmbiguousKey(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)
	systemUUID := uuid.New()

	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "clone-a")))
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.seedSibling(t), &systemUUID, "clone-b")))

	_, _, err := f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)

	// The lookup caps the rows it reads, so a third clone reads the same as two.
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.seedSibling(t), &systemUUID, "clone-c")))

	_, _, err = f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)
}

func TestSetAMTDetailRefusesOutOfScope(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, nil, "Intel Core i7-12700K")))

	otherTenant := dbtx.WithTenant(context.Background(), uuid.New(), false)

	tests := []struct {
		name     string
		ctx      context.Context
		deviceID device.DeviceID
		wantErr  error
	}{
		{"unknown device", f.ctx, uuid.New(), device.ErrHardwareNotFound},
		{"no tenant on the context", context.Background(), f.deviceID, dbtx.ErrTenantRequired},
		{"another tenant's device", otherTenant, f.deviceID, device.ErrHardwareNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.hardware.SetAMTDetail(tt.ctx, tt.deviceID, "OptiPlex 7090", "16.1.25")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
