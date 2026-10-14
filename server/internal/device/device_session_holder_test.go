package device_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestADeviceReadNamesTheTechnicianHoldingItsSession(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	held := testutil.SeedDevice(t, ctx, store, site.ID)
	free := testutil.SeedDevice(t, ctx, store, site.ID)
	alice := testutil.SeedUser(t, ctx, store)
	testutil.SeedAgentSession(t, ctx, store, held.ID, alice.ID)

	got, err := devices.Get(ctx, held.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Session, "a held host says so")
	assert.Equal(t, alice.ID, got.Session.HolderID)
	assert.Equal(t, alice.DisplayName, got.Session.HolderName)
	assert.WithinDuration(t, time.Now(), got.Session.StartedAt, time.Minute)

	other, err := devices.Get(ctx, free.ID)
	require.NoError(t, err)
	assert.Nil(t, other.Session, "a free host carries no holder")

	listed, err := devices.List(ctx, device.Filter{})
	require.NoError(t, err)
	var seen int
	for _, d := range listed {
		switch d.ID {
		case held.ID:
			seen++
			require.NotNil(t, d.Session, "the fleet list shows the holder too")
			assert.Equal(t, alice.ID, d.Session.HolderID)
		case free.ID:
			seen++
			assert.Nil(t, d.Session)
		}
	}
	assert.Equal(t, 2, seen)
}

func TestAHolderWithNoDisplayNameIsNamedByEmail(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	held := testutil.SeedDevice(t, ctx, store, site.ID)
	bob := testutil.SeedUser(t, ctx, store)
	bob.DisplayName = ""
	require.NoError(t, testutil.NewTestUsers(t, store).Upsert(ctx, bob))
	testutil.SeedAgentSession(t, ctx, store, held.ID, bob.ID)

	got, err := devices.Get(ctx, held.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Session)
	assert.Equal(t, bob.Email, got.Session.HolderName)
}
