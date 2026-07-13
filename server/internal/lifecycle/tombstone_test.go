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
func denied(t *testing.T, ts *TombstoneStore, ctx context.Context, org, device uuid.UUID) bool {
	t.Helper()
	got, err := ts.IsDeviceTombstoned(ctx, org, device)
	require.NoError(t, err)
	return got
}

func TestTombstoneDeviceIsRejectedAfterRecording(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	org := uuid.New()
	device := uuid.New()

	// Before any tombstone the id is live.
	assert.False(t, denied(t, ts, ctx, org, device), "untombstoned device must be live")

	require.NoError(t, ts.TombstoneDevice(ctx, org, device, nil))
	assert.True(t, denied(t, ts, ctx, org, device), "tombstoned device must be rejected")

	// A different device in the same org stays live.
	assert.False(t, denied(t, ts, ctx, org, uuid.New()), "sibling device must stay live")
}

func TestTombstoneDeviceIsIdempotent(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	org := uuid.New()
	device := uuid.New()
	by := uuid.New()

	require.NoError(t, ts.TombstoneDevice(ctx, org, device, &by))
	// Re-recording the same tombstone (e.g. a resumed purge) must not error.
	require.NoError(t, ts.TombstoneDevice(ctx, org, device, &by))

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

func TestTombstoneOrgSupersedesDevices(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	org := uuid.New()
	device := uuid.New()

	require.NoError(t, ts.TombstoneOrg(ctx, org, nil))

	// The org tombstone rejects every device in the org, even ones never
	// individually tombstoned.
	orgTombstoned, err := ts.IsOrgTombstoned(ctx, org)
	require.NoError(t, err)
	assert.True(t, orgTombstoned)
	assert.True(t, denied(t, ts, ctx, org, device), "org tombstone must supersede for its devices")

	// Another org is untouched.
	assert.False(t, denied(t, ts, ctx, uuid.New(), device), "org tombstone must not leak across tenants")
}

func TestTombstoneListAllRoundTrips(t *testing.T) {
	t.Parallel()
	ts, ctx := newTombstoneFixture(t)

	org := uuid.New()
	device := uuid.New()
	require.NoError(t, ts.TombstoneDevice(ctx, org, device, nil))
	require.NoError(t, ts.TombstoneOrg(ctx, org, nil))

	all, err := ts.ListAll(ctx)
	require.NoError(t, err)

	var sawDevice, sawOrg bool
	for _, tomb := range all {
		if tomb.OrgID != org {
			continue
		}
		switch tomb.Scope {
		case ScopeDevice:
			if tomb.DeviceID != nil && *tomb.DeviceID == device {
				sawDevice = true
			}
		case ScopeOrg:
			sawOrg = true
			assert.Nil(t, tomb.DeviceID, "org tombstone carries no device id")
		}
	}
	assert.True(t, sawDevice, "device tombstone must round-trip through ListAll")
	assert.True(t, sawOrg, "org tombstone must round-trip through ListAll")
}
