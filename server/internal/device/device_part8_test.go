package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"testing"
	"time"
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
		require.Len(t, obs.calls, 2)
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
		require.Len(t, obs.calls, 2)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}

func TestInstrumentedLogs_AllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedLogs(&memLogs{}, obs)
		require.NoError(t, r.Upsert(ctx, uuid.New(), nil))
		_, _, err := r.Query(ctx, uuid.New(), device.LogFilter{})
		require.NoError(t, err)
		_, err = r.HasRecent(ctx, uuid.New(), time.Minute)
		require.NoError(t, err)
		require.Len(t, obs.calls, 3)
		for _, c := range obs.calls {
			assert.True(t, c.ok)
		}
	})

	t.Run("error", func(t *testing.T) {
		obs := &fakeObserver{}
		r := device.NewInstrumentedLogs(&memLogs{failEvery: true}, obs)
		assert.Error(t, r.Upsert(ctx, uuid.New(), nil))
		_, _, err := r.Query(ctx, uuid.New(), device.LogFilter{})
		assert.Error(t, err)
		_, err = r.HasRecent(ctx, uuid.New(), time.Minute)
		assert.Error(t, err)
		require.Len(t, obs.calls, 3)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
