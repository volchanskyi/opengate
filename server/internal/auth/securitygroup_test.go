package auth_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newTestSGRepo returns a Postgres-backed auth.SecurityGroupRepository against
// the per-test isolated schema created by testutil.NewTestStore.
func newTestSGRepo(t *testing.T) (auth.SecurityGroupRepository, db.Store) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return testutil.NewTestSecurityGroups(t, store), store
}

func seedUser(t *testing.T, ctx context.Context, store db.Store) *auth.User {
	t.Helper()
	return testutil.SeedUser(t, ctx, store)
}

func TestPostgresSecurityGroups_CRUD(t *testing.T) {
	t.Parallel()
	repo, _ := newTestSGRepo(t)
	ctx := context.Background()

	t.Run("migration seeds Administrators group", func(t *testing.T) {
		g, err := repo.Get(ctx, auth.AdminGroupID)
		require.NoError(t, err)
		assert.Equal(t, "Administrators", g.Name)
		assert.True(t, g.IsSystem)
		assert.False(t, g.CreatedAt.IsZero())
	})

	t.Run("create and get", func(t *testing.T) {
		g := &auth.SecurityGroup{
			ID:          uuid.New(),
			Name:        "Operators-" + uuid.New().String()[:8],
			Description: "Can manage devices",
		}
		require.NoError(t, repo.Create(ctx, g))

		got, err := repo.Get(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, g.Name, got.Name)
		assert.Equal(t, "Can manage devices", got.Description)
		assert.False(t, got.IsSystem)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, uuid.New())
		assert.ErrorIs(t, err, auth.ErrSecurityGroupNotFound)
	})

	t.Run("list includes seeded and created groups", func(t *testing.T) {
		groups, err := repo.List(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(groups), 2)
	})

	t.Run("delete non-system group", func(t *testing.T) {
		g := &auth.SecurityGroup{ID: uuid.New(), Name: "Temporary-" + uuid.New().String()[:8]}
		require.NoError(t, repo.Create(ctx, g))
		require.NoError(t, repo.Delete(ctx, g.ID))
		_, err := repo.Get(ctx, g.ID)
		assert.ErrorIs(t, err, auth.ErrSecurityGroupNotFound)
	})

	t.Run("cannot delete system group", func(t *testing.T) {
		err := repo.Delete(ctx, auth.AdminGroupID)
		assert.ErrorIs(t, err, auth.ErrSystemGroup)
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, uuid.New())
		assert.ErrorIs(t, err, auth.ErrSecurityGroupNotFound)
	})

	t.Run("duplicate name fails", func(t *testing.T) {
		g := &auth.SecurityGroup{ID: uuid.New(), Name: "Administrators"}
		err := repo.Create(ctx, g)
		assert.Error(t, err)
	})
}

func TestPostgresSecurityGroups_Members(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	ctx := context.Background()

	t.Run("add and list members", func(t *testing.T) {
		u1 := seedUser(t, ctx, store)
		u2 := seedUser(t, ctx, store)
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u1.ID))
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u2.ID))

		members, err := repo.ListMembers(ctx, auth.AdminGroupID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(members), 2)
		// Member view omits password hash; verify display fields populated.
		for _, m := range members {
			assert.NotEmpty(t, m.Email)
		}
	})

	t.Run("is user in group", func(t *testing.T) {
		u := seedUser(t, ctx, store)
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u.ID))

		ok, err := repo.IsUserInGroup(ctx, u.ID, auth.AdminGroupID)
		require.NoError(t, err)
		assert.True(t, ok)

		nonMember := seedUser(t, ctx, store)
		ok, err = repo.IsUserInGroup(ctx, nonMember.ID, auth.AdminGroupID)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("add is idempotent", func(t *testing.T) {
		u := seedUser(t, ctx, store)
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u.ID))
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u.ID))
	})

	t.Run("remove member", func(t *testing.T) {
		u1 := seedUser(t, ctx, store)
		u2 := seedUser(t, ctx, store)
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u1.ID))
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u2.ID))

		require.NoError(t, repo.RemoveMember(ctx, auth.AdminGroupID, u1.ID))

		ok, err := repo.IsUserInGroup(ctx, u1.ID, auth.AdminGroupID)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("count members", func(t *testing.T) {
		g := &auth.SecurityGroup{ID: uuid.New(), Name: "Count-" + uuid.New().String()[:8]}
		require.NoError(t, repo.Create(ctx, g))

		count, err := repo.CountMembers(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		u := seedUser(t, ctx, store)
		require.NoError(t, repo.AddMember(ctx, g.ID, u.ID))
		count, err = repo.CountMembers(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("remove not-found member returns ErrMemberNotFound", func(t *testing.T) {
		g := &auth.SecurityGroup{ID: uuid.New(), Name: "RemNF-" + uuid.New().String()[:8]}
		require.NoError(t, repo.Create(ctx, g))
		u := seedUser(t, ctx, store)
		require.NoError(t, repo.AddMember(ctx, g.ID, u.ID))
		err := repo.RemoveMember(ctx, g.ID, uuid.New())
		assert.ErrorIs(t, err, auth.ErrMemberNotFound)
	})
}

func TestPostgresSecurityGroups_LastAdminProtection(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	ctx := context.Background()

	u := seedUser(t, ctx, store)
	require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u.ID))

	count, err := repo.CountMembers(ctx, auth.AdminGroupID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = repo.RemoveMember(ctx, auth.AdminGroupID, u.ID)
	assert.ErrorIs(t, err, auth.ErrLastAdmin)

	ok, err := repo.IsUserInGroup(ctx, u.ID, auth.AdminGroupID)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPostgresSecurityGroups_SyncIsAdmin(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	ctx := context.Background()
	u := seedUser(t, ctx, store)

	// Initially not admin.
	got, err := testutil.NewTestUsers(t, store).Get(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAdmin)

	// Add to Administrators — users.is_admin should flip to true.
	require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u.ID))
	got, err = testutil.NewTestUsers(t, store).Get(ctx, u.ID)
	require.NoError(t, err)
	assert.True(t, got.IsAdmin)

	// Add a second admin so we can remove the first without tripping ErrLastAdmin.
	u2 := seedUser(t, ctx, store)
	require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u2.ID))

	// Remove from Administrators — users.is_admin should flip to false.
	require.NoError(t, repo.RemoveMember(ctx, auth.AdminGroupID, u.ID))
	got, err = testutil.NewTestUsers(t, store).Get(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAdmin)
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

// memSG is an in-memory SecurityGroupRepository for testing the Instrumented
// decorator without needing Postgres.
type memSG struct {
	failEvery bool
}

func (m *memSG) maybeFail() error {
	if m.failEvery {
		return sql.ErrConnDone
	}
	return nil
}
func (m *memSG) Create(_ context.Context, _ *auth.SecurityGroup) error { return m.maybeFail() }
func (m *memSG) Get(_ context.Context, _ auth.SecurityGroupID) (*auth.SecurityGroup, error) {
	if err := m.maybeFail(); err != nil {
		return nil, err
	}
	return &auth.SecurityGroup{ID: auth.AdminGroupID, Name: "Administrators"}, nil
}
func (m *memSG) List(_ context.Context) ([]*auth.SecurityGroup, error) {
	if err := m.maybeFail(); err != nil {
		return nil, err
	}
	return nil, nil
}
func (m *memSG) Delete(_ context.Context, _ auth.SecurityGroupID) error { return m.maybeFail() }
func (m *memSG) AddMember(_ context.Context, _ auth.SecurityGroupID, _ uuid.UUID) error {
	return m.maybeFail()
}
func (m *memSG) RemoveMember(_ context.Context, _ auth.SecurityGroupID, _ uuid.UUID) error {
	return m.maybeFail()
}
func (m *memSG) ListMembers(_ context.Context, _ auth.SecurityGroupID) ([]*auth.Member, error) {
	if err := m.maybeFail(); err != nil {
		return nil, err
	}
	return nil, nil
}
func (m *memSG) IsUserInGroup(_ context.Context, _ uuid.UUID, _ auth.SecurityGroupID) (bool, error) {
	if err := m.maybeFail(); err != nil {
		return false, err
	}
	return true, nil
}
func (m *memSG) CountMembers(_ context.Context, _ auth.SecurityGroupID) (int, error) {
	if err := m.maybeFail(); err != nil {
		return 0, err
	}
	return 0, nil
}

func TestInstrumentedSecurityGroups_ObservesAllMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("success paths", func(t *testing.T) {
		obs := &fakeObserver{}
		repo := auth.NewInstrumentedSecurityGroups(&memSG{}, obs)

		require.NoError(t, repo.Create(ctx, &auth.SecurityGroup{}))
		_, err := repo.Get(ctx, auth.AdminGroupID)
		require.NoError(t, err)
		_, err = repo.List(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.Delete(ctx, auth.AdminGroupID))
		require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, uuid.New()))
		require.NoError(t, repo.RemoveMember(ctx, auth.AdminGroupID, uuid.New()))
		_, err = repo.ListMembers(ctx, auth.AdminGroupID)
		require.NoError(t, err)
		_, err = repo.IsUserInGroup(ctx, uuid.New(), auth.AdminGroupID)
		require.NoError(t, err)
		_, err = repo.CountMembers(ctx, auth.AdminGroupID)
		require.NoError(t, err)

		require.Len(t, obs.calls, 9)
		wantOps := []string{
			"auth.SecurityGroup.Create",
			"auth.SecurityGroup.Get",
			"auth.SecurityGroup.List",
			"auth.SecurityGroup.Delete",
			"auth.SecurityGroup.AddMember",
			"auth.SecurityGroup.RemoveMember",
			"auth.SecurityGroup.ListMembers",
			"auth.SecurityGroup.IsUserInGroup",
			"auth.SecurityGroup.CountMembers",
		}
		for i, op := range wantOps {
			assert.Equal(t, op, obs.calls[i].op)
			assert.True(t, obs.calls[i].ok)
		}
	})

	t.Run("error paths flip ok to false", func(t *testing.T) {
		obs := &fakeObserver{}
		failRepo := auth.NewInstrumentedSecurityGroups(&memSG{failEvery: true}, obs)

		require.Error(t, failRepo.Create(ctx, &auth.SecurityGroup{}))
		_, err := failRepo.Get(ctx, auth.AdminGroupID)
		require.Error(t, err)
		_, err = failRepo.List(ctx)
		require.Error(t, err)
		require.Error(t, failRepo.Delete(ctx, auth.AdminGroupID))
		require.Error(t, failRepo.AddMember(ctx, auth.AdminGroupID, uuid.New()))
		require.Error(t, failRepo.RemoveMember(ctx, auth.AdminGroupID, uuid.New()))
		_, err = failRepo.ListMembers(ctx, auth.AdminGroupID)
		require.Error(t, err)
		_, err = failRepo.IsUserInGroup(ctx, uuid.New(), auth.AdminGroupID)
		require.Error(t, err)
		_, err = failRepo.CountMembers(ctx, auth.AdminGroupID)
		require.Error(t, err)

		require.Len(t, obs.calls, 9)
		for _, c := range obs.calls {
			assert.False(t, c.ok)
		}
	})
}
