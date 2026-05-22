package device_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// seedOwner inserts a user we can use as Group.OwnerID without exercising
// auth-aggregate-specific helpers.
func seedOwner(t *testing.T, ctx context.Context, store db.Store) uuid.UUID {
	t.Helper()
	u := testutil.SeedUser(t, ctx, store)
	return u.ID
}

func newRepos(t *testing.T) (device.Repository, device.GroupRepository, device.HardwareRepository, device.LogsRepository, db.Store) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return testutil.NewTestDevices(t, store),
		testutil.NewTestGroups(t, store),
		testutil.NewTestHardware(t, store),
		testutil.NewTestLogs(t, store),
		store
}

func TestPostgresDevices_CRUD(t *testing.T) {
	t.Parallel()
	devices, groups, _, _, store := newRepos(t)
	ctx := context.Background()
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

func TestPostgresGroups_CRUD(t *testing.T) {
	t.Parallel()
	_, groups, _, _, store := newRepos(t)
	ctx := context.Background()
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
	devices, groups, hardware, _, store := newRepos(t)
	ctx := context.Background()
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

func TestPostgresLogs_UpsertQueryHasRecent(t *testing.T) {
	t.Parallel()
	devices, groups, _, logs, store := newRepos(t)
	ctx := context.Background()
	owner := seedOwner(t, ctx, store)

	g := &device.Group{ID: uuid.New(), Name: "g-" + uuid.New().String()[:8], OwnerID: owner}
	require.NoError(t, groups.Create(ctx, g))
	d := &device.Device{ID: uuid.New(), GroupID: g.ID, Hostname: "logs", OS: "linux", Status: device.StatusOffline}
	require.NoError(t, devices.Upsert(ctx, d))

	entries := []device.LogEntry{
		{Timestamp: "2026-05-20T10:00:00Z", Level: "INFO", Target: "app", Message: "started"},
		{Timestamp: "2026-05-20T10:01:00Z", Level: "WARN", Target: "app", Message: "slow request"},
		{Timestamp: "2026-05-20T10:02:00Z", Level: "ERROR", Target: "app", Message: "boom"},
	}
	require.NoError(t, logs.Upsert(ctx, d.ID, entries))

	t.Run("query unfiltered", func(t *testing.T) {
		got, total, err := logs.Query(ctx, d.ID, device.LogFilter{})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		assert.Len(t, got, 3)
	})

	t.Run("query by severity WARN+", func(t *testing.T) {
		got, total, err := logs.Query(ctx, d.ID, device.LogFilter{Level: "WARN"})
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.Len(t, got, 2)
	})

	t.Run("query with search", func(t *testing.T) {
		got, total, err := logs.Query(ctx, d.ID, device.LogFilter{Search: "boom"})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, "boom", got[0].Message)
	})

	t.Run("has recent within window", func(t *testing.T) {
		ok, err := logs.HasRecent(ctx, d.ID, 1*time.Hour)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("has recent outside window", func(t *testing.T) {
		ok, err := logs.HasRecent(ctx, d.ID, -1*time.Hour)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("upsert replaces", func(t *testing.T) {
		require.NoError(t, logs.Upsert(ctx, d.ID, []device.LogEntry{{Timestamp: "2026-05-20T11:00:00Z", Level: "INFO", Target: "x", Message: "second"}}))
		_, total, err := logs.Query(ctx, d.ID, device.LogFilter{})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
	})
}

// fakeObserver records every Observe call for the Instrumented decorator tests.
type fakeObserver struct {
	calls []observerCall
}

type observerCall struct {
	op       string
	duration time.Duration
	ok       bool
}

func (f *fakeObserver) Observe(op string, d time.Duration, ok bool) {
	f.calls = append(f.calls, observerCall{op: op, duration: d, ok: ok})
}

// In-memory stubs for Instrumented decorator tests.

type memDevices struct {
	failEvery bool
}

func (m *memDevices) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}
func (m *memDevices) Upsert(_ context.Context, _ *device.Device) error { return m.maybeFail() }
func (m *memDevices) Get(_ context.Context, _ device.DeviceID) (*device.Device, error) {
	return &device.Device{}, m.maybeFail()
}
func (m *memDevices) List(_ context.Context, _ device.GroupID) ([]*device.Device, error) {
	return nil, m.maybeFail()
}
func (m *memDevices) ListAll(_ context.Context) ([]*device.Device, error) { return nil, m.maybeFail() }
func (m *memDevices) ListForOwner(_ context.Context, _ uuid.UUID) ([]*device.Device, error) {
	return nil, m.maybeFail()
}
func (m *memDevices) Delete(_ context.Context, _ device.DeviceID) error { return m.maybeFail() }
func (m *memDevices) UpdateGroup(_ context.Context, _ device.DeviceID, _ device.GroupID) error {
	return m.maybeFail()
}
func (m *memDevices) SetStatus(_ context.Context, _ device.DeviceID, _ device.DeviceStatus) error {
	return m.maybeFail()
}
func (m *memDevices) ResetAllStatuses(_ context.Context) error { return m.maybeFail() }

type memGroups struct{ failEvery bool }

func (m *memGroups) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}
func (m *memGroups) Create(_ context.Context, _ *device.Group) error { return m.maybeFail() }
func (m *memGroups) Get(_ context.Context, _ device.GroupID) (*device.Group, error) {
	return &device.Group{}, m.maybeFail()
}
func (m *memGroups) List(_ context.Context, _ uuid.UUID) ([]*device.Group, error) {
	return nil, m.maybeFail()
}
func (m *memGroups) Delete(_ context.Context, _ device.GroupID) error { return m.maybeFail() }

type memHardware struct{ failEvery bool }

func (m *memHardware) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}
func (m *memHardware) Upsert(_ context.Context, _ *device.Hardware) error { return m.maybeFail() }
func (m *memHardware) Get(_ context.Context, _ device.DeviceID) (*device.Hardware, error) {
	return &device.Hardware{}, m.maybeFail()
}

type memLogs struct{ failEvery bool }

func (m *memLogs) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}
func (m *memLogs) Upsert(_ context.Context, _ device.DeviceID, _ []device.LogEntry) error {
	return m.maybeFail()
}
func (m *memLogs) Query(_ context.Context, _ device.DeviceID, _ device.LogFilter) ([]device.LogEntry, int, error) {
	return nil, 0, m.maybeFail()
}
func (m *memLogs) HasRecent(_ context.Context, _ device.DeviceID, _ time.Duration) (bool, error) {
	return false, m.maybeFail()
}

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
