package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"testing"
)

func TestInstrumentedHardware_AllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedHardware(&memHardware{}, obs)
		require.NoError(t, r.Upsert(ctx, &device.Hardware{}))
		_, err := r.Get(ctx, uuid.New())
		require.NoError(t, err)
		_, _, err = r.ResolveBySystemUUID(ctx, uuid.New())
		require.NoError(t, err)
		require.NoError(t, r.SetAMTDetail(ctx, uuid.New(), "OptiPlex 7090", "16.1.25"))
		require.Len(t, obs.calls, 4)
		for _, c := range obs.calls {
			assert.True(t, c.ok)
		}
	})

	t.Run("error", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedHardware(&memHardware{failEvery: true}, obs)
		assert.Error(t, r.Upsert(ctx, &device.Hardware{}))
		_, err := r.Get(ctx, uuid.New())
		assert.Error(t, err)
		_, _, err = r.ResolveBySystemUUID(ctx, uuid.New())
		assert.Error(t, err)
		assert.Error(t, r.SetAMTDetail(ctx, uuid.New(), "OptiPlex 7090", "16.1.25"))
		require.Len(t, obs.calls, 4)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
