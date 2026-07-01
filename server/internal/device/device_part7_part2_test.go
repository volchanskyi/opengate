package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"testing"
)

func TestInstrumentedGroups_AllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedGroups(&memGroups{}, obs)
		require.NoError(t, r.Create(ctx, &device.Group{}))
		_, err := r.Get(ctx, uuid.New())
		require.NoError(t, err)
		_, err = r.List(ctx, uuid.New())
		require.NoError(t, err)
		require.NoError(t, r.Delete(ctx, uuid.New()))
		require.Len(t, obs.calls, 4)
		for _, c := range obs.calls {
			assert.True(t, c.ok)
		}
	})

	t.Run("error", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedGroups(&memGroups{failEvery: true}, obs)
		assert.Error(t, r.Create(ctx, &device.Group{}))
		_, err := r.Get(ctx, uuid.New())
		assert.Error(t, err)
		_, err = r.List(ctx, uuid.New())
		assert.Error(t, err)
		assert.Error(t, r.Delete(ctx, uuid.New()))
		require.Len(t, obs.calls, 4)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
