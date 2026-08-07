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
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// amtConnectionState reads back a row's device link and status directly, since
// the repository is a write-only port — the device read serves AMT state to
// callers.
func amtConnectionState(t *testing.T, ctx context.Context, store *db.PostgresStore, id uuid.UUID) (uuid.UUID, db.DeviceStatus, bool) {
	t.Helper()
	tenant, ok := dbtx.TenantFromContext(ctx)
	require.True(t, ok)

	var deviceID uuid.UUID
	var status db.DeviceStatus
	err := store.DB().QueryRowContext(ctx,
		`SELECT device_id, status FROM amt_devices WHERE tenant_id = $1 AND uuid = $2`,
		tenant.TenantID, id).Scan(&deviceID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", false
	}
	require.NoError(t, err)
	return deviceID, status, true
}

// seedLinkedDevice creates a managed device in ctx's tenant to hang an AMT
// connection off, since a connection with no device is never persisted.
func seedLinkedDevice(t *testing.T, ctx context.Context, store *db.PostgresStore) *device.Device {
	t.Helper()
	group := testutil.SeedGroup(t, ctx, store)
	return testutil.SeedDevice(t, ctx, store, group.ID)
}

func TestPostgres_AMTConnectionState(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestAMTDevices(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

	t.Run("upsert records the device link and status", func(t *testing.T) {
		dev := seedLinkedDevice(t, ctx, store)
		d := &db.AMTDevice{UUID: uuid.New(), DeviceID: dev.ID, Status: db.StatusOnline}
		require.NoError(t, repo.Upsert(ctx, d))

		gotDevice, gotStatus, found := amtConnectionState(t, ctx, store, d.UUID)
		require.True(t, found)
		assert.Equal(t, dev.ID, gotDevice)
		assert.Equal(t, db.StatusOnline, gotStatus)
	})

	t.Run("upsert refreshes an existing connection", func(t *testing.T) {
		dev := seedLinkedDevice(t, ctx, store)
		id := uuid.New()
		require.NoError(t, repo.Upsert(ctx, &db.AMTDevice{UUID: id, DeviceID: dev.ID, Status: db.StatusOnline}))
		require.NoError(t, repo.Upsert(ctx, &db.AMTDevice{UUID: id, DeviceID: dev.ID, Status: db.StatusOffline}))

		gotDevice, gotStatus, found := amtConnectionState(t, ctx, store, id)
		require.True(t, found)
		assert.Equal(t, dev.ID, gotDevice)
		assert.Equal(t, db.StatusOffline, gotStatus)
	})

	t.Run("upsert requires a tenant", func(t *testing.T) {
		dev := seedLinkedDevice(t, ctx, store)
		err := repo.Upsert(context.Background(), &db.AMTDevice{UUID: uuid.New(), DeviceID: dev.ID, Status: db.StatusOnline})
		assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	})

	t.Run("set status", func(t *testing.T) {
		dev := seedLinkedDevice(t, ctx, store)
		d := &db.AMTDevice{UUID: uuid.New(), DeviceID: dev.ID, Status: db.StatusOnline}
		require.NoError(t, repo.Upsert(ctx, d))

		require.NoError(t, repo.SetStatus(ctx, d.UUID, db.StatusOffline))
		_, gotStatus, found := amtConnectionState(t, ctx, store, d.UUID)
		require.True(t, found)
		assert.Equal(t, db.StatusOffline, gotStatus)
	})

	t.Run("set status not found", func(t *testing.T) {
		err := repo.SetStatus(ctx, uuid.New(), db.StatusOnline)
		assert.ErrorIs(t, err, amt.ErrAMTDeviceNotFound)
	})
}

func TestPostgresAMTDevices_TenantDeny(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestAMTDevices(t, store)
	tenantB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	deviceB := testutil.SeedAMTDevice(t, ctxB, store, seedLinkedDevice(t, ctxB, store).ID)

	// Tenant A cannot see, let alone change, tenant B's AMT connection.
	_, _, found := amtConnectionState(t, ctxA, store, deviceB.UUID)
	assert.False(t, found)
	assert.ErrorIs(t, repo.SetStatus(ctxA, deviceB.UUID, db.StatusOnline), amt.ErrAMTDeviceNotFound)
	assert.ErrorIs(t, repo.SetStatus(context.Background(), deviceB.UUID, db.StatusOnline), dbtx.ErrTenantRequired)
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

func TestInstrumented_ObservesUpsertError(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{upsertErr: sql.ErrConnDone}, obs)

	require.Error(t, repo.Upsert(context.Background(), &db.AMTDevice{UUID: uuid.New()}))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "amt.Upsert", obs.calls[0].op)
	assert.False(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesSetStatus(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{}, obs)

	require.NoError(t, repo.SetStatus(context.Background(), uuid.New(), db.StatusOnline))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "amt.SetStatus", obs.calls[0].op)
}

func TestInstrumented_ObservesSetStatusError(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := amt.NewInstrumented(&memRepo{setErr: sql.ErrConnDone}, obs)

	require.Error(t, repo.SetStatus(context.Background(), uuid.New(), db.StatusOnline))

	require.Len(t, obs.calls, 1)
	assert.False(t, obs.calls[0].ok)
}
