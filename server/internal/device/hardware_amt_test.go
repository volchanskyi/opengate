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

// amtFixture is one tenant with one managed device, plus the hardware repository
// under test. Every case here needs exactly that.
type amtFixture struct {
	hardware device.HardwareRepository
	store    *db.PostgresStore
	ctx      context.Context
	deviceID device.DeviceID
	orgID    uuid.UUID
}

// seedSibling adds a second device to the same tenant, for cases that need two.
func (f amtFixture) seedSibling(t *testing.T) device.DeviceID {
	t.Helper()
	owner := testutil.SeedUser(t, f.ctx, f.store)
	group := testutil.SeedGroup(t, f.ctx, f.store, owner.ID)
	return testutil.SeedDevice(t, f.ctx, f.store, group.ID).ID
}

// newAMTFixture seeds a device in orgID's tenant, or the default tenant when
// orgID is uuid.Nil.
func newAMTFixture(t *testing.T, orgID uuid.UUID) amtFixture {
	t.Helper()
	_, _, hardware, store := newRepos(t)

	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	if orgID != uuid.Nil {
		testutil.EnsureOrganization(t, context.Background(), store, orgID, "Tenant "+orgID.String()[:8])
		ctx = dbtx.WithTenant(context.Background(), orgID, false)
	} else {
		orgID = dbtx.DefaultOrgID
	}

	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)
	dev := testutil.SeedDevice(t, ctx, store, group.ID)
	return amtFixture{hardware: hardware, store: store, ctx: ctx, deviceID: dev.ID, orgID: orgID}
}

// report is what the agent's hardware inventory writes: host facts plus the AMT
// presence it reads off the Management Engine.
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

// TestHardwareAMTColumnsSurviveBothWriters is the regression this change is most
// likely to break: the agent's inventory report and the server's WSMAN detail
// write share one row and must not blank each other's columns.
func TestHardwareAMTColumnsSurviveBothWriters(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)
	systemUUID := uuid.New()

	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i7-12700K")))
	require.NoError(t, f.hardware.SetAMTDetail(f.ctx, f.deviceID, "OptiPlex 7090", "16.1.25"))

	// The AMT write must leave every agent-sourced column intact.
	hw, err := f.hardware.Get(f.ctx, f.deviceID)
	require.NoError(t, err)
	assert.Equal(t, "Intel Core i7-12700K", hw.CPUModel)
	assert.Equal(t, 8, hw.CPUCores)
	require.NotNil(t, hw.AMTAvailable)
	assert.True(t, *hw.AMTAvailable)
	assert.Equal(t, "16.1.30.2260", hw.AMTVersion)
	assert.Equal(t, "OptiPlex 7090", hw.AMTModel)
	assert.Equal(t, "16.1.25", hw.AMTFirmware)

	// A second agent report must leave the AMT-sourced columns intact.
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i9-13900K")))

	hw, err = f.hardware.Get(f.ctx, f.deviceID)
	require.NoError(t, err)
	assert.Equal(t, "Intel Core i9-13900K", hw.CPUModel)
	assert.Equal(t, "OptiPlex 7090", hw.AMTModel, "an agent report must not blank the WSMAN-sourced model")
	assert.Equal(t, "16.1.25", hw.AMTFirmware, "an agent report must not blank the WSMAN-sourced firmware")
}

// TestHardwareUpsertAMTPresenceSkew covers version skew in both directions: an
// agent too old to report AMT presence must not erase what a newer one
// established, while a host that genuinely has no Management Engine must be able
// to say so.
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

			// The join key survives either way, or the AMT connection is orphaned.
			gotDevice, _, err := f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
			require.NoError(t, err)
			assert.Equal(t, f.deviceID, gotDevice)
		})
	}
}

// TestResolveBySystemUUIDCrossesOrganizations proves the CIRA lookup works from
// outside any request tenant — the whole point of the join key.
func TestResolveBySystemUUIDCrossesOrganizations(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.New())
	systemUUID := uuid.New()
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "Intel Core i5-1145G7")))

	// No tenant on the context at all — exactly what an MPS connection has.
	gotDevice, gotOrg, err := f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
	require.NoError(t, err)
	assert.Equal(t, f.deviceID, gotDevice)
	assert.Equal(t, f.orgID, gotOrg, "the resolved organization is what scopes every later write")
}

func TestResolveBySystemUUIDUnknownKey(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)

	_, _, err := f.hardware.ResolveBySystemUUID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)
}

// TestResolveBySystemUUIDAmbiguousKey covers cloned disk images, which hand the
// same SMBIOS UUID to several hosts. An ambiguous key is not an identity, so it
// must resolve to nothing rather than to an arbitrary device.
func TestResolveBySystemUUIDAmbiguousKey(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)
	systemUUID := uuid.New()

	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, &systemUUID, "clone-a")))
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.seedSibling(t), &systemUUID, "clone-b")))

	_, _, err := f.hardware.ResolveBySystemUUID(context.Background(), systemUUID)
	assert.ErrorIs(t, err, device.ErrHardwareNotFound)
}

// TestSetAMTDetailRefusesOutOfScope covers every way the WSMAN write can miss:
// an unknown device, no tenant at all, and a tenant that does not own the row.
func TestSetAMTDetailRefusesOutOfScope(t *testing.T) {
	t.Parallel()
	f := newAMTFixture(t, uuid.Nil)
	require.NoError(t, f.hardware.Upsert(f.ctx, report(f.deviceID, nil, "Intel Core i7-12700K")))

	otherOrg := dbtx.WithTenant(context.Background(), uuid.New(), false)

	tests := []struct {
		name     string
		ctx      context.Context
		deviceID device.DeviceID
		wantErr  error
	}{
		{"unknown device", f.ctx, uuid.New(), device.ErrHardwareNotFound},
		{"no tenant on the context", context.Background(), f.deviceID, dbtx.ErrTenantRequired},
		{"another tenant's device", otherOrg, f.deviceID, device.ErrHardwareNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.hardware.SetAMTDetail(tt.ctx, tt.deviceID, "OptiPlex 7090", "16.1.25")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
