package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestPostgres_UserCRUD(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestUsers(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

	t.Run("upsert and get", func(t *testing.T) {
		u := &auth.User{
			ID:           uuid.New(),
			Email:        "alice-" + uuid.New().String()[:8] + "@example.com",
			PasswordHash: "argon2",
			DisplayName:  "Alice",
			IsAdmin:      true,
		}
		require.NoError(t, repo.Upsert(ctx, u))

		got, err := repo.Get(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, u.ID, got.ID)
		assert.Equal(t, u.Email, got.Email)
		assert.Equal(t, u.PasswordHash, got.PasswordHash)
		assert.Equal(t, u.DisplayName, got.DisplayName)
		assert.True(t, got.IsAdmin)
		assert.False(t, got.CreatedAt.IsZero())
		assert.False(t, got.UpdatedAt.IsZero())
	})

	t.Run("upsert updates existing", func(t *testing.T) {
		u := &auth.User{ID: uuid.New(), Email: "update-" + uuid.New().String()[:8] + "@example.com", DisplayName: "Before"}
		require.NoError(t, repo.Upsert(ctx, u))

		u.DisplayName = "After"
		require.NoError(t, repo.Upsert(ctx, u))

		got, err := repo.Get(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "After", got.DisplayName)
	})

	t.Run("get by email", func(t *testing.T) {
		email := "byemail-" + uuid.New().String()[:8] + "@example.com"
		u := &auth.User{ID: uuid.New(), Email: email}
		require.NoError(t, repo.Upsert(ctx, u))

		got, err := repo.GetByEmail(ctx, email)
		require.NoError(t, err)
		assert.Equal(t, u.ID, got.ID)
	})

	t.Run("get by email not found", func(t *testing.T) {
		_, err := repo.GetByEmail(ctx, "nope-"+uuid.New().String()[:8]+"@example.com")
		assert.True(t, errors.Is(err, auth.ErrUserNotFound))
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, uuid.New())
		assert.True(t, errors.Is(err, auth.ErrUserNotFound))
	})

	t.Run("list", func(t *testing.T) {
		users, err := repo.List(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(users), 2)
	})

	t.Run("delete", func(t *testing.T) {
		u := &auth.User{ID: uuid.New(), Email: "delete-" + uuid.New().String()[:8] + "@example.com"}
		require.NoError(t, repo.Upsert(ctx, u))
		require.NoError(t, repo.Delete(ctx, u.ID))
		_, err := repo.Get(ctx, u.ID)
		assert.True(t, errors.Is(err, auth.ErrUserNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, uuid.New())
		assert.True(t, errors.Is(err, auth.ErrUserNotFound))
	})
}

func TestPostgresUsers_TenantDeny(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestUsers(t, store)
	orgB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), orgB, false)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	userA := &auth.User{ID: uuid.New(), Email: "a-" + uuid.New().String()[:8] + "@example.com"}
	userB := &auth.User{ID: uuid.New(), Email: "b-" + uuid.New().String()[:8] + "@example.com"}
	require.NoError(t, repo.Upsert(ctxA, userA))
	require.NoError(t, repo.Upsert(ctxB, userB))

	_, err := repo.Get(ctxA, userB.ID)
	assert.ErrorIs(t, err, auth.ErrUserNotFound)
	_, err = repo.GetByEmail(ctxA, userB.Email)
	assert.ErrorIs(t, err, auth.ErrUserNotFound)

	users, err := repo.List(ctxA)
	require.NoError(t, err)
	seen := map[uuid.UUID]bool{}
	for _, u := range users {
		seen[u.ID] = true
		assert.Equal(t, dbtx.DefaultOrgID, u.OrgID)
	}
	assert.True(t, seen[userA.ID])
	assert.False(t, seen[userB.ID])

	_, err = repo.Get(context.Background(), userA.ID)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}

// fakeUserObserver records every Observe call for the Instrumented decorator test.
type fakeUserObserver struct {
	calls []userObserverCall
}

type userObserverCall struct {
	op       string
	duration time.Duration
	ok       bool
}

func (f *fakeUserObserver) Observe(op string, d time.Duration, ok bool) {
	f.calls = append(f.calls, userObserverCall{op: op, duration: d, ok: ok})
}

// memUserRepo is an in-memory auth.UserRepository for testing the
// Instrumented decorator.
type memUserRepo struct {
	upsertErr error
	getErr    error
	listErr   error
	deleteErr error
	users     map[uuid.UUID]*auth.User
}

func (m *memUserRepo) Upsert(_ context.Context, u *auth.User) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	if m.users == nil {
		m.users = make(map[uuid.UUID]*auth.User)
	}
	m.users[u.ID] = u
	return nil
}

func (m *memUserRepo) Get(_ context.Context, id uuid.UUID) (*auth.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	u, ok := m.users[id]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	return u, nil
}

func (m *memUserRepo) GetByEmail(_ context.Context, _ string) (*auth.User, error) {
	return nil, m.getErr
}

func (m *memUserRepo) List(_ context.Context) ([]*auth.User, error) {
	return nil, m.listErr
}

func (m *memUserRepo) Delete(_ context.Context, _ uuid.UUID) error { return m.deleteErr }

func TestInstrumentedUsers_ObservesUpsert(t *testing.T) {
	t.Parallel()
	obs := &fakeUserObserver{}
	repo := auth.NewInstrumentedUsers(&memUserRepo{}, obs)

	require.NoError(t, repo.Upsert(context.Background(), &auth.User{ID: uuid.New()}))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "auth.User.Upsert", obs.calls[0].op)
	assert.True(t, obs.calls[0].ok)
}

func TestInstrumentedUsers_ObservesGetError(t *testing.T) {
	t.Parallel()
	obs := &fakeUserObserver{}
	repo := auth.NewInstrumentedUsers(&memUserRepo{getErr: sql.ErrConnDone}, obs)

	_, err := repo.Get(context.Background(), uuid.New())
	require.Error(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "auth.User.Get", obs.calls[0].op)
	assert.False(t, obs.calls[0].ok)
}

func TestInstrumentedUsers_ObservesGetByEmail(t *testing.T) {
	t.Parallel()
	obs := &fakeUserObserver{}
	repo := auth.NewInstrumentedUsers(&memUserRepo{getErr: auth.ErrUserNotFound}, obs)

	_, err := repo.GetByEmail(context.Background(), "x@example.com")
	require.Error(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "auth.User.GetByEmail", obs.calls[0].op)
}

func TestInstrumentedUsers_ObservesList(t *testing.T) {
	t.Parallel()
	obs := &fakeUserObserver{}
	repo := auth.NewInstrumentedUsers(&memUserRepo{}, obs)

	_, err := repo.List(context.Background())
	require.NoError(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "auth.User.List", obs.calls[0].op)
}

func TestInstrumentedUsers_ObservesDelete(t *testing.T) {
	t.Parallel()
	obs := &fakeUserObserver{}
	repo := auth.NewInstrumentedUsers(&memUserRepo{}, obs)

	require.NoError(t, repo.Delete(context.Background(), uuid.New()))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "auth.User.Delete", obs.calls[0].op)
}
