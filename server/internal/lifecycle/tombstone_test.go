package lifecycle

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newTombstoneFixture returns a fresh deny-list store over a throwaway Postgres
// plus a background context, the common start of every tombstone test.
func newTombstoneFixture(t *testing.T) (*TombstoneStore, context.Context) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return NewTombstoneStore(store.DB()), context.Background()
}

// denied reports whether a device is on the deny-list, failing the test on a
// query error so every call site is a single boolean assertion.
func denied(t *testing.T, ts *TombstoneStore, ctx context.Context, tenant, device uuid.UUID) bool {
	t.Helper()
	got, err := ts.IsDeviceTombstoned(ctx, tenant, device)
	require.NoError(t, err)
	return got
}

func TestTombstoneDeviceIsRejectedAfterRecording(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	tenant := uuid.New()
	device := uuid.New()

	// Before any tombstone the id is live.
	assert.False(t, denied(t, ts, ctx, tenant, device), "untombstoned device must be live")

	require.NoError(t, ts.TombstoneDevice(ctx, tenant, device, nil))
	assert.True(t, denied(t, ts, ctx, tenant, device), "tombstoned device must be rejected")

	// A different device in the same tenant stays live.
	assert.False(t, denied(t, ts, ctx, tenant, uuid.New()), "sibling device must stay live")
}

func TestTombstoneDeviceIsIdempotent(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	tenant := uuid.New()
	device := uuid.New()
	by := uuid.New()

	require.NoError(t, ts.TombstoneDevice(ctx, tenant, device, &by))
	// Re-recording the same tombstone (e.g. a resumed purge) must not error.
	require.NoError(t, ts.TombstoneDevice(ctx, tenant, device, &by))

	all, err := ts.ListAll(ctx)
	require.NoError(t, err)
	count := 0
	for _, tomb := range all {
		if tomb.DeviceID != nil && *tomb.DeviceID == device {
			count++
		}
	}
	assert.Equal(t, 1, count, "idempotent tombstone must not duplicate rows")
}

func TestTombstoneTenantSupersedesDevices(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	tenant := uuid.New()
	device := uuid.New()

	require.NoError(t, ts.TombstoneTenant(ctx, tenant, nil))

	// The tenant tombstone rejects every device in the tenant, even ones never
	// individually tombstoned.
	tenantTombstoned, err := ts.IsTenantTombstoned(ctx, tenant)
	require.NoError(t, err)
	assert.True(t, tenantTombstoned)
	assert.True(t, denied(t, ts, ctx, tenant, device), "tenant tombstone must supersede for its devices")

	// Another tenant is untouched.
	assert.False(t, denied(t, ts, ctx, uuid.New(), device), "tenant tombstone must not leak across tenants")
}

func TestTombstoneListAllRoundTrips(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	tenant := uuid.New()
	device := uuid.New()
	require.NoError(t, ts.TombstoneDevice(ctx, tenant, device, nil))
	require.NoError(t, ts.TombstoneTenant(ctx, tenant, nil))

	all, err := ts.ListAll(ctx)
	require.NoError(t, err)

	var sawDevice, sawTenant bool
	for _, tomb := range all {
		if tomb.TenantID != tenant {
			continue
		}
		switch tomb.Scope {
		case ScopeDevice:
			if tomb.DeviceID != nil && *tomb.DeviceID == device {
				sawDevice = true
			}
		case ScopeTenant:
			sawTenant = true
			assert.Nil(t, tomb.DeviceID, "tenant tombstone carries no device id")
		}
	}
	assert.True(t, sawDevice, "device tombstone must round-trip through ListAll")
	assert.True(t, sawTenant, "tenant tombstone must round-trip through ListAll")
}
