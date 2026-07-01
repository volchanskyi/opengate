package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"testing"
)

func TestInstrumentedDevices_AllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success paths", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedDevices(&memDevices{}, obs)

		require.NoError(t, r.Upsert(ctx, &device.Device{}))
		_, err := r.Get(ctx, uuid.New())
		require.NoError(t, err)
		_, err = r.List(ctx, uuid.New())
		require.NoError(t, err)
		_, err = r.ListAll(ctx)
		require.NoError(t, err)
		_, err = r.ListForOwner(ctx, uuid.New())
		require.NoError(t, err)
		require.NoError(t, r.Delete(ctx, uuid.New()))
		require.NoError(t, r.UpdateGroup(ctx, uuid.New(), uuid.New()))
		require.NoError(t, r.SetStatus(ctx, uuid.New(), device.StatusOnline))
		require.NoError(t, r.ResetAllStatuses(ctx))

		require.Len(t, obs.calls, 9)
		for _, c := range obs.calls {
			assert.True(t, c.ok)
		}
	})

	t.Run("error paths", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedDevices(&memDevices{failEvery: true}, obs)

		assert.Error(t, r.Upsert(ctx, &device.Device{}))
		_, err := r.Get(ctx, uuid.New())
		assert.Error(t, err)
		_, err = r.List(ctx, uuid.New())
		assert.Error(t, err)
		_, err = r.ListAll(ctx)
		assert.Error(t, err)
		_, err = r.ListForOwner(ctx, uuid.New())
		assert.Error(t, err)
		assert.Error(t, r.Delete(ctx, uuid.New()))
		assert.Error(t, r.UpdateGroup(ctx, uuid.New(), uuid.New()))
		assert.Error(t, r.SetStatus(ctx, uuid.New(), device.StatusOnline))
		assert.Error(t, r.ResetAllStatuses(ctx))

		require.Len(t, obs.calls, 9)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
