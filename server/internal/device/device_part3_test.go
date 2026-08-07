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
	_, sites, _, _ := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

	g := &device.Site{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8]}
	require.NoError(t, sites.Create(ctx, g))

	t.Run("get", func(t *testing.T) {
		got, err := sites.Get(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, g.Name, got.Name)
	})

	t.Run("get missing", func(t *testing.T) {
		_, err := sites.Get(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrSiteNotFound)
	})

	t.Run("list returns the tenant's sites", func(t *testing.T) {
		gs, err := sites.List(ctx, uuid.Nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(gs), 1)
	})

	t.Run("delete", func(t *testing.T) {
		g2 := &device.Site{ID: uuid.New(), Name: "del-" + uuid.New().String()[:8]}
		require.NoError(t, sites.Create(ctx, g2))
		require.NoError(t, sites.Delete(ctx, g2.ID))
		_, err := sites.Get(ctx, g2.ID)
		assert.ErrorIs(t, err, device.ErrSiteNotFound)
	})

	t.Run("delete missing", func(t *testing.T) {
		err := sites.Delete(ctx, uuid.New())
		assert.ErrorIs(t, err, device.ErrSiteNotFound)
	})
}

func TestPostgresHardware_UpsertAndGet(t *testing.T) {
	t.Parallel()
	devices, sites, hardware, _ := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

	g := &device.Site{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8]}
	require.NoError(t, sites.Create(ctx, g))
	d := &device.Device{ID: uuid.New(), SiteID: g.ID, Hostname: "hw", OS: "linux", Status: device.StatusOffline}
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
