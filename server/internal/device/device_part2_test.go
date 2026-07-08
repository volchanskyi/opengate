package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"testing"
)

func TestPostgresDevices_CRUD(t *testing.T) {
	t.Parallel()
	devices, groups, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := seedOwner(t, ctx, store)

	g := &device.Group{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8], OwnerID: owner}
	require.NoError(t, groups.Create(ctx, g))

	t.Run("upsert and get", func(t *testing.T) {
		d := &device.Device{
			ID:       uuid.New(),
			GroupID:  g.ID,
			Hostname: "h-" + uuid.New().String()[:8],
			OS:       "linux",
			Status:   device.StatusOffline,
		}
		require.NoError(t, devices.Upsert(ctx, d))

		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, d.Hostname, got.Hostname)
		assert.Equal(t, device.StatusOffline, got.Status)
		assert.Equal(t, []string{}, got.Capabilities)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := devices.Get(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	})

	t.Run("list by group", func(t *testing.T) {
		ds, err := devices.List(ctx, g.ID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(ds), 1)
	})

	t.Run("list all", func(t *testing.T) {
		ds, err := devices.ListAll(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(ds), 1)
	})

	t.Run("list for owner", func(t *testing.T) {
		ds, err := devices.ListForOwner(ctx, owner)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(ds), 1)
	})

	t.Run("update group", func(t *testing.T) {
		d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "moveme", OS: "linux", Status: device.StatusOffline}
		require.NoError(t, devices.Upsert(ctx, d))

		g2 := &device.Group{ID: uuid.New(), Name: "g2-" + uuid.New().String()[:8], OwnerID: owner}
		require.NoError(t, groups.Create(ctx, g2))

		require.NoError(t, devices.UpdateGroup(ctx, d.ID, g2.ID))
		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, g2.ID, got.GroupID)
	})

	t.Run("set status flips to online", func(t *testing.T) {
		d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "stat", OS: "linux", Status: device.StatusOffline}
		require.NoError(t, devices.Upsert(ctx, d))

		require.NoError(t, devices.SetStatus(ctx, d.ID, device.StatusOnline))
		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, device.StatusOnline, got.Status)
	})

	t.Run("reset all statuses turns online to offline", func(t *testing.T) {
		d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "reset", OS: "linux", Status: device.StatusOnline}
		require.NoError(t, devices.Upsert(ctx, d))
		require.NoError(t, devices.ResetAllStatuses(ctx))

		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, device.StatusOffline, got.Status)
	})

	t.Run("delete", func(t *testing.T) {
		d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "del", OS: "linux", Status: device.StatusOffline}
		require.NoError(t, devices.Upsert(ctx, d))
		require.NoError(t, devices.Delete(ctx, d.ID))

		_, err := devices.Get(ctx, d.ID)
		assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	})

	t.Run("delete missing", func(t *testing.T) {
		err := devices.Delete(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	})
}
