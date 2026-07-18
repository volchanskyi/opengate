package device_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
)

func TestPostgresDevices_Maintenance(t *testing.T) {
	t.Parallel()
	devices, groups, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := seedOwner(t, ctx, store)

	g := &device.Group{ID: uuid.New(), Name: "gm-" + uuid.New().String()[:8], OwnerID: owner}
	require.NoError(t, groups.Create(ctx, g))

	newDevice := func(host string) *device.Device {
		d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: host, OS: "linux", Status: device.StatusOnline}
		require.NoError(t, devices.Upsert(ctx, d))
		return d
	}

	t.Run("default active", func(t *testing.T) {
		d := newDevice("mnt-default")
		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.False(t, got.MaintenanceOn)
		assert.Nil(t, got.MaintenanceSince)
		assert.Nil(t, got.MaintenanceBy)
		assert.Empty(t, got.MaintenanceReason)
	})

	t.Run("enable sets since/by/reason", func(t *testing.T) {
		d := newDevice("mnt-enable")
		require.NoError(t, devices.SetMaintenance(ctx, d.ID, true, owner, "kernel upgrade"))

		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.True(t, got.MaintenanceOn)
		require.NotNil(t, got.MaintenanceSince)
		require.NotNil(t, got.MaintenanceBy)
		assert.Equal(t, owner, *got.MaintenanceBy)
		assert.Equal(t, "kernel upgrade", got.MaintenanceReason)
	})

	t.Run("re-enable preserves since, updates reason", func(t *testing.T) {
		d := newDevice("mnt-reenable")
		require.NoError(t, devices.SetMaintenance(ctx, d.ID, true, owner, "first"))
		first, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		require.NotNil(t, first.MaintenanceSince)

		require.NoError(t, devices.SetMaintenance(ctx, d.ID, true, owner, "second"))
		second, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		require.NotNil(t, second.MaintenanceSince)
		assert.Equal(t, first.MaintenanceSince.UnixNano(), second.MaintenanceSince.UnixNano(),
			"entering-maintenance timestamp must not reset when the reason is edited in place")
		assert.Equal(t, "second", second.MaintenanceReason)
	})

	t.Run("disable clears since/by/reason", func(t *testing.T) {
		d := newDevice("mnt-disable")
		require.NoError(t, devices.SetMaintenance(ctx, d.ID, true, owner, "work"))
		require.NoError(t, devices.SetMaintenance(ctx, d.ID, false, owner, ""))

		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.False(t, got.MaintenanceOn)
		assert.Nil(t, got.MaintenanceSince)
		assert.Nil(t, got.MaintenanceBy)
		assert.Empty(t, got.MaintenanceReason)
	})

	t.Run("upsert preserves maintenance state", func(t *testing.T) {
		d := newDevice("mnt-upsert")
		require.NoError(t, devices.SetMaintenance(ctx, d.ID, true, owner, "reboot"))

		// A re-registration (Upsert) must never clobber the operator-set state.
		d.Hostname = "mnt-upsert-renamed"
		require.NoError(t, devices.Upsert(ctx, d))

		got, err := devices.Get(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "mnt-upsert-renamed", got.Hostname)
		assert.True(t, got.MaintenanceOn)
		assert.Equal(t, "reboot", got.MaintenanceReason)
	})

	t.Run("count reflects enabled devices", func(t *testing.T) {
		before, err := devices.CountInMaintenance(ctx)
		require.NoError(t, err)

		a := newDevice("mnt-count-a")
		b := newDevice("mnt-count-b")
		require.NoError(t, devices.SetMaintenance(ctx, a.ID, true, owner, ""))
		require.NoError(t, devices.SetMaintenance(ctx, b.ID, true, owner, ""))

		after, err := devices.CountInMaintenance(ctx)
		require.NoError(t, err)
		assert.Equal(t, before+2, after)

		require.NoError(t, devices.SetMaintenance(ctx, a.ID, false, owner, ""))
		afterDisable, err := devices.CountInMaintenance(ctx)
		require.NoError(t, err)
		assert.Equal(t, before+1, afterDisable)
	})

	t.Run("set on missing device is not found", func(t *testing.T) {
		err := devices.SetMaintenance(ctx, uuid.New(), true, owner, "")
		assert.ErrorIs(t, err, device.ErrDeviceNotFound)
	})
}

// TestInstrumentedDevices_Maintenance covers the maintenance methods on the
// observation decorator (success + error), asserting each is timed under its
// operation name.
func TestInstrumentedDevices_Maintenance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedDevices(&memDevices{}, obs)

		require.NoError(t, r.SetMaintenance(ctx, uuid.New(), true, uuid.New(), "work"))
		_, err := r.CountInMaintenance(ctx)
		require.NoError(t, err)

		require.Len(t, obs.calls, 2)
		assert.Equal(t, "device.Device.SetMaintenance", obs.calls[0].op)
		assert.Equal(t, "device.Device.CountInMaintenance", obs.calls[1].op)
		for _, c := range obs.calls {
			assert.True(t, c.ok)
		}
	})

	t.Run("error", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedDevices(&memDevices{failEvery: true}, obs)

		assert.Error(t, r.SetMaintenance(ctx, uuid.New(), true, uuid.New(), "work"))
		_, err := r.CountInMaintenance(ctx)
		assert.Error(t, err)

		require.Len(t, obs.calls, 2)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
