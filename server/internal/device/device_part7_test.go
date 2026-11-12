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

// instrumentedCalls is every call the instrumented repository wraps, in the order it records them.
func instrumentedCalls(ctx context.Context) []func(device.Repository) error {
	return []func(device.Repository) error{
		func(r device.Repository) error { return r.Upsert(ctx, &device.Device{}) },
		func(r device.Repository) error { _, err := r.Get(ctx, uuid.New()); return err },
		func(r device.Repository) error { _, err := r.TenantForDevice(ctx, uuid.New()); return err },
		func(r device.Repository) error { _, err := r.List(ctx, device.Filter{SiteID: uuid.New()}); return err },
		func(r device.Repository) error { _, err := r.List(ctx, device.Filter{}); return err },
		func(r device.Repository) error { _, err := r.GetByAMTUUID(ctx, uuid.New()); return err },
		func(r device.Repository) error { return r.Delete(ctx, uuid.New()) },
		func(r device.Repository) error { return r.UpdateSite(ctx, uuid.New(), uuid.New()) },
		func(r device.Repository) error { return r.SetStatus(ctx, uuid.New(), device.StatusOnline) },
		func(r device.Repository) error { return r.ResetAllStatuses(ctx) },
		func(r device.Repository) error {
			_, _, err := r.RefreshPresence(ctx, []device.DeviceID{uuid.New()}, time.Minute)
			return err
		},
	}
}

func TestInstrumentedDevices_AllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		fail bool
	}{{"success paths", false}, {"error paths", true}} {
		t.Run(tc.name, func(t *testing.T) {
			obs := &fakeObserver{}
			r := device.NewInstrumentedDevices(&memDevices{failEvery: tc.fail}, obs)

			calls := instrumentedCalls(ctx)
			for i, call := range calls {
				assert.Equalf(t, tc.fail, call(r) != nil, "call %d", i)
			}

			require.Len(t, obs.calls, 11)
			for _, c := range obs.calls {
				assert.Equal(t, !tc.fail, c.ok)
			}
		})
	}
}

func TestInstrumentedDevicesNameEachOperationAndPassCountsThrough(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	r := device.NewInstrumentedDevices(&memDevices{}, obs)
	for _, call := range instrumentedCalls(context.Background()) {
		require.NoError(t, call(r))
	}

	refreshed, offline, err := r.RefreshPresence(context.Background(), []device.DeviceID{uuid.New()}, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, refreshed, "the inner store's counts pass through")
	assert.Equal(t, 2, offline)
	assert.Equal(t, "device.Device.TenantForDevice", obs.calls[2].op)
	assert.Equal(t, "device.Device.RefreshPresence", obs.calls[10].op)
}
