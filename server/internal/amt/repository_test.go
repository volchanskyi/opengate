package amt_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestPostgres_AMTDeviceCRUD(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestAMTDevices(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

	t.Run("upsert and get", func(t *testing.T) {
		d := &db.AMTDevice{
			UUID:     uuid.New(),
			Hostname: "amt-host-1",
			Model:    "vPro i7",
			Firmware: "16.1.0",
			Status:   db.StatusOnline,
		}
		require.NoError(t, repo.Upsert(ctx, d))

		got, err := repo.Get(ctx, d.UUID)
		require.NoError(t, err)
		assert.Equal(t, d.UUID, got.UUID)
		assert.Equal(t, "amt-host-1", got.Hostname)
		assert.Equal(t, "vPro i7", got.Model)
		assert.Equal(t, "16.1.0", got.Firmware)
		assert.Equal(t, db.StatusOnline, got.Status)
		assert.False(t, got.LastSeen.IsZero())
	})

	t.Run("upsert preserves non-empty fields", func(t *testing.T) {
		id := uuid.New()
		d := &db.AMTDevice{UUID: id, Hostname: "host-a", Model: "Model-X", Firmware: "1.0", Status: db.StatusOnline}
		require.NoError(t, repo.Upsert(ctx, d))

		d2 := &db.AMTDevice{UUID: id, Status: db.StatusOffline}
		require.NoError(t, repo.Upsert(ctx, d2))

		got, err := repo.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "host-a", got.Hostname)
		assert.Equal(t, "Model-X", got.Model)
		assert.Equal(t, db.StatusOffline, got.Status)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, uuid.New())
		assert.True(t, errors.Is(err, amt.ErrAMTDeviceNotFound))
	})

	t.Run("list", func(t *testing.T) {
		id1 := uuid.New()
		id2 := uuid.New()
		require.NoError(t, repo.Upsert(ctx, &db.AMTDevice{UUID: id1, Hostname: "list-1", Status: db.StatusOnline}))
		require.NoError(t, repo.Upsert(ctx, &db.AMTDevice{UUID: id2, Hostname: "list-2", Status: db.StatusOffline}))

		devices, err := repo.List(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(devices), 2)
	})

	t.Run("set status", func(t *testing.T) {
		d := &db.AMTDevice{UUID: uuid.New(), Status: db.StatusOnline}
		require.NoError(t, repo.Upsert(ctx, d))

		require.NoError(t, repo.SetStatus(ctx, d.UUID, db.StatusOffline))
		got, err := repo.Get(ctx, d.UUID)
		require.NoError(t, err)
		assert.Equal(t, db.StatusOffline, got.Status)
	})

	t.Run("set status not found", func(t *testing.T) {
		err := repo.SetStatus(ctx, uuid.New(), db.StatusOnline)
		assert.True(t, errors.Is(err, amt.ErrAMTDeviceNotFound))
	})
}

func TestPostgresAMTDevices_TenantDeny(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestAMTDevices(t, store)
	orgB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), orgB, false)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	deviceA := testutil.SeedAMTDevice(t, ctxA, store)
	deviceB := testutil.SeedAMTDevice(t, ctxB, store)

	_, err := repo.Get(ctxA, deviceB.UUID)
	assert.ErrorIs(t, err, amt.ErrAMTDeviceNotFound)
	devices, err := repo.List(ctxA)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Equal(t, deviceA.UUID, devices[0].UUID)

	_, err = repo.Get(context.Background(), deviceA.UUID)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}

// fakeObserver records every Observe call for the Instrumented decorator test.
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

// memRepo is an in-memory amt.Repository for testing the Instrumented decorator.
type memRepo struct {
	upsertErr error
	getErr    error
	listErr   error
	setErr    error
	devices   map[uuid.UUID]*db.AMTDevice
}

func (m *memRepo) Upsert(_ context.Context, d *db.AMTDevice) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	if m.devices == nil {
		m.devices = make(map[uuid.UUID]*db.AMTDevice)
	}
	m.devices[d.UUID] = d
	return nil
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (*db.AMTDevice, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	d, ok := m.devices[id]
	if !ok {
		return nil, amt.ErrAMTDeviceNotFound
	}
	return d, nil
}

func (m *memRepo) List(_ context.Context) ([]*db.AMTDevice, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]*db.AMTDevice, 0, len(m.devices))
	for _, d := range m.devices {
		out = append(out, d)
	}
	return out, nil
}

func (m *memRepo) SetStatus(_ context.Context, _ uuid.UUID, _ db.DeviceStatus) error {
	return m.setErr
}

func TestInstrumented_ObservesUpsert(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{}, obs)

	require.NoError(t, repo.Upsert(context.Background(), &db.AMTDevice{UUID: uuid.New()}))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "amt.Upsert", obs.calls[0].op)
	assert.True(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesGetError(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{getErr: sql.ErrConnDone}, obs)

	_, err := repo.Get(context.Background(), uuid.New())
	require.Error(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "amt.Get", obs.calls[0].op)
	assert.False(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesList(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{}, obs)

	_, err := repo.List(context.Background())
	require.NoError(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "amt.List", obs.calls[0].op)
}

func TestInstrumented_ObservesSetStatus(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{}, obs)

	require.NoError(t, repo.SetStatus(context.Background(), uuid.New(), db.StatusOnline))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "amt.SetStatus", obs.calls[0].op)
}
