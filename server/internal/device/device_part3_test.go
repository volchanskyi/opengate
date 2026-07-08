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

func TestPostgresGroups_CRUD(t *testing.T) {
	t.Parallel()
	_, groups, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := seedOwner(t, ctx, store)

	g := &device.Group{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8], OwnerID: owner}
	require.NoError(t, groups.Create(ctx, g))

	t.Run("get", func(t *testing.T) {
		got, err := groups.Get(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, g.Name, got.Name)
	})

	t.Run("get missing", func(t *testing.T) {
		_, err := groups.Get(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrGroupNotFound)
	})

	t.Run("list for owner", func(t *testing.T) {
		gs, err := groups.List(ctx, owner)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(gs), 1)
	})

	t.Run("delete", func(t *testing.T) {
		g2 := &device.Group{ID: uuid.New(), Name: "del-" + uuid.New().String()[:8], OwnerID: owner}
		require.NoError(t, groups.Create(ctx, g2))
		require.NoError(t, groups.Delete(ctx, g2.ID))
		_, err := groups.Get(ctx, g2.ID)
		assert.ErrorIs(t, err, device.ErrGroupNotFound)
	})

	t.Run("delete missing", func(t *testing.T) {
		err := groups.Delete(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrGroupNotFound)
	})
}

func TestPostgresHardware_UpsertAndGet(t *testing.T) {
	t.Parallel()
	devices, groups, hardware, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := seedOwner(t, ctx, store)

	g := &device.Group{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8], OwnerID: owner}
	require.NoError(t, groups.Create(ctx, g))
	d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "hw", OS: "linux", Status: device.StatusOffline}
	require.NoError(t, devices.Upsert(ctx, d))

	hw := &device.Hardware{
		DeviceID:    d.ID,
		CPUModel:    "Intel i7",
		CPUCores:    8,
		RAMTotalMB:  16384,
		DiskTotalMB: 512000,
		DiskFreeMB:  100000,
		NetworkInterfaces: []device.NetworkInterfaceInfo{
			{Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"192.168.1.10"}},
		},
	}
	require.NoError(t, hardware.Upsert(ctx, hw))

	t.Run("get", func(t *testing.T) {
		got, err := hardware.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "Intel i7", got.CPUModel)
		assert.Equal(t, 8, got.CPUCores)
		require.Len(t, got.NetworkInterfaces, 1)
		assert.Equal(t, "eth0", got.NetworkInterfaces[0].Name)
	})

	t.Run("get missing", func(t *testing.T) {
		_, err := hardware.Get(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrHardwareNotFound)
	})
}
