package updater_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

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

// memDeviceUpdates is an in-memory DeviceUpdateRepository for testing the
// Instrumented decorator without needing Postgres.
type memDeviceUpdates struct {
	createErr error
	setErr    error
	listErr   error
	items     []*updater.DeviceUpdate
}

func (m *memDeviceUpdates) Create(_ context.Context, du *updater.DeviceUpdate) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.items = append(m.items, du)
	return nil
}

func (m *memDeviceUpdates) SetStatus(_ context.Context, _ uuid.UUID, _ string, _ updater.Status, _ string) error {
	return m.setErr
}

func (m *memDeviceUpdates) ListByVersion(_ context.Context, _ string) ([]*updater.DeviceUpdate, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.items, nil
}

// memEnrollments is an in-memory EnrollmentTokenRepository for the decorator test.
type memEnrollments struct {
	createErr error
	getErr    error
	listErr   error
	deleteErr error
	incErr    error
	items     []*updater.EnrollmentToken
}

func (m *memEnrollments) Create(_ context.Context, t *updater.EnrollmentToken) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.items = append(m.items, t)
	return nil
}

func (m *memEnrollments) GetByToken(_ context.Context, _ string) (*updater.EnrollmentToken, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if len(m.items) == 0 {
		return nil, sql.ErrNoRows
	}
	return m.items[0], nil
}

func (m *memEnrollments) List(_ context.Context, _ uuid.UUID) ([]*updater.EnrollmentToken, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.items, nil
}

func (m *memEnrollments) Delete(_ context.Context, _ uuid.UUID) error            { return m.deleteErr }
func (m *memEnrollments) IncrementUseCount(_ context.Context, _ uuid.UUID) error { return m.incErr }

func TestPostgresDeviceUpdates_CRUD(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestDeviceUpdates(t, store)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	device := testutil.SeedDevice(t, ctx, store, group.ID)

	t.Run("create populates ID and persists", func(t *testing.T) {
		du := &updater.DeviceUpdate{
			DeviceID: device.ID,
			Version:  "1.2.3",
			Status:   updater.StatusPending,
		}
		require.NoError(t, repo.Create(ctx, du))
		assert.Greater(t, du.ID, int64(0))

		list, err := repo.ListByVersion(ctx, "1.2.3")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, updater.StatusPending, list[0].Status)
		assert.Nil(t, list[0].AckedAt)
	})

	t.Run("set status to success acks", func(t *testing.T) {
		require.NoError(t, repo.SetStatus(ctx, device.ID, "1.2.3", updater.StatusSuccess, ""))

		list, err := repo.ListByVersion(ctx, "1.2.3")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, updater.StatusSuccess, list[0].Status)
		require.NotNil(t, list[0].AckedAt)
	})

	t.Run("set status on missing record returns not found", func(t *testing.T) {
		err := repo.SetStatus(ctx, uuid.New(), "9.9.9", updater.StatusSuccess, "")
		require.Error(t, err)
	})
}

func TestPostgresEnrollment_CRUD(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestEnrollment(t, store)
	ctx := context.Background()

	creator := testutil.SeedUser(t, ctx, store)

	t.Run("create and get by token", func(t *testing.T) {
		tok := &updater.EnrollmentToken{
			ID:        uuid.New(),
			Token:     "test-token-abc",
			Label:     "test",
			CreatedBy: creator.ID,
			MaxUses:   10,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		require.NoError(t, repo.Create(ctx, tok))

		got, err := repo.GetByToken(ctx, "test-token-abc")
		require.NoError(t, err)
		assert.Equal(t, tok.ID, got.ID)
		assert.Equal(t, "test", got.Label)
		assert.Equal(t, 0, got.UseCount)
	})

	t.Run("get unknown token returns sql.ErrNoRows-like error", func(t *testing.T) {
		_, err := repo.GetByToken(ctx, "nonexistent-token")
		require.Error(t, err)
	})

	t.Run("list scopes to created_by", func(t *testing.T) {
		other := testutil.SeedUser(t, ctx, store)
		t1 := &updater.EnrollmentToken{ID: uuid.New(), Token: uuid.New().String(), CreatedBy: creator.ID, MaxUses: 1, ExpiresAt: time.Now().Add(time.Hour)}
		t2 := &updater.EnrollmentToken{ID: uuid.New(), Token: uuid.New().String(), CreatedBy: other.ID, MaxUses: 1, ExpiresAt: time.Now().Add(time.Hour)}
		require.NoError(t, repo.Create(ctx, t1))
		require.NoError(t, repo.Create(ctx, t2))

		mine, err := repo.List(ctx, creator.ID)
		require.NoError(t, err)
		for _, tok := range mine {
			assert.Equal(t, creator.ID, tok.CreatedBy)
		}
	})

	t.Run("increment use count", func(t *testing.T) {
		tok := &updater.EnrollmentToken{ID: uuid.New(), Token: uuid.New().String(), CreatedBy: creator.ID, MaxUses: 5, ExpiresAt: time.Now().Add(time.Hour)}
		require.NoError(t, repo.Create(ctx, tok))

		require.NoError(t, repo.IncrementUseCount(ctx, tok.ID))
		require.NoError(t, repo.IncrementUseCount(ctx, tok.ID))

		got, err := repo.GetByToken(ctx, tok.Token)
		require.NoError(t, err)
		assert.Equal(t, 2, got.UseCount)
	})

	t.Run("increment on missing returns not found", func(t *testing.T) {
		require.Error(t, repo.IncrementUseCount(ctx, uuid.New()))
	})

	t.Run("delete then get returns error", func(t *testing.T) {
		tok := &updater.EnrollmentToken{ID: uuid.New(), Token: uuid.New().String(), CreatedBy: creator.ID, MaxUses: 1, ExpiresAt: time.Now().Add(time.Hour)}
		require.NoError(t, repo.Create(ctx, tok))

		require.NoError(t, repo.Delete(ctx, tok.ID))
		_, err := repo.GetByToken(ctx, tok.Token)
		require.Error(t, err)
	})
}

func TestInstrumentedDeviceUpdates_ObservesAllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success paths", func(t *testing.T) {
		obs := &fakeObserver{}
		repo := updater.NewInstrumentedDeviceUpdates(&memDeviceUpdates{}, obs)

		require.NoError(t, repo.Create(ctx, &updater.DeviceUpdate{DeviceID: uuid.New(), Version: "1"}))
		require.NoError(t, repo.SetStatus(ctx, uuid.New(), "1", updater.StatusSuccess, ""))
		_, err := repo.ListByVersion(ctx, "1")
		require.NoError(t, err)

		require.Len(t, obs.calls, 3)
		assert.Equal(t, "updater.DeviceUpdate.Create", obs.calls[0].op)
		assert.True(t, obs.calls[0].ok)
		assert.Equal(t, "updater.DeviceUpdate.SetStatus", obs.calls[1].op)
		assert.True(t, obs.calls[1].ok)
		assert.Equal(t, "updater.DeviceUpdate.ListByVersion", obs.calls[2].op)
		assert.True(t, obs.calls[2].ok)
	})

	t.Run("error paths flip ok to false", func(t *testing.T) {
		obs := &fakeObserver{}
		failRepo := updater.NewInstrumentedDeviceUpdates(&memDeviceUpdates{
			createErr: sql.ErrConnDone,
			setErr:    sql.ErrConnDone,
			listErr:   sql.ErrConnDone,
		}, obs)

		require.Error(t, failRepo.Create(ctx, &updater.DeviceUpdate{}))
		require.Error(t, failRepo.SetStatus(ctx, uuid.New(), "1", updater.StatusFailed, "boom"))
		_, err := failRepo.ListByVersion(ctx, "1")
		require.Error(t, err)

		require.Len(t, obs.calls, 3)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}

func TestInstrumentedEnrollment_ObservesAllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success paths", func(t *testing.T) {
		obs := &fakeObserver{}
		// Seed one item so GetByToken/List succeed.
		seeded := &memEnrollments{items: []*updater.EnrollmentToken{{ID: uuid.New(), Token: "tok"}}}
		repo := updater.NewInstrumentedEnrollment(seeded, obs)

		require.NoError(t, repo.Create(ctx, &updater.EnrollmentToken{ID: uuid.New()}))
		_, err := repo.GetByToken(ctx, "tok")
		require.NoError(t, err)
		_, err = repo.List(ctx, uuid.New())
		require.NoError(t, err)
		require.NoError(t, repo.Delete(ctx, uuid.New()))
		require.NoError(t, repo.IncrementUseCount(ctx, uuid.New()))

		require.Len(t, obs.calls, 5)
		wantOps := []string{
			"updater.Enrollment.Create",
			"updater.Enrollment.GetByToken",
			"updater.Enrollment.List",
			"updater.Enrollment.Delete",
			"updater.Enrollment.IncrementUseCount",
		}
		for i, op := range wantOps {
			assert.Equal(t, op, obs.calls[i].op)
			assert.True(t, obs.calls[i].ok)
		}
	})

	t.Run("error paths flip ok to false", func(t *testing.T) {
		obs := &fakeObserver{}
		failRepo := updater.NewInstrumentedEnrollment(&memEnrollments{
			createErr: sql.ErrConnDone,
			getErr:    sql.ErrConnDone,
			listErr:   sql.ErrConnDone,
			deleteErr: sql.ErrConnDone,
			incErr:    sql.ErrConnDone,
		}, obs)

		require.Error(t, failRepo.Create(ctx, &updater.EnrollmentToken{}))
		_, err := failRepo.GetByToken(ctx, "x")
		require.Error(t, err)
		_, err = failRepo.List(ctx, uuid.New())
		require.Error(t, err)
		require.Error(t, failRepo.Delete(ctx, uuid.New()))
		require.Error(t, failRepo.IncrementUseCount(ctx, uuid.New()))

		require.Len(t, obs.calls, 5)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
