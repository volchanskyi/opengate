package auth_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
)

// newTestSGRepo returns a Postgres-backed auth.SecurityGroupRepository against
// the per-test isolated schema created by testutil.NewTestStore.
func newTestSGRepo(t *testing.T) (auth.SecurityGroupRepository, *db.PostgresStore) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return testutil.NewTestSecurityGroups(t, store), store
}

func seedUser(t *testing.T, ctx context.Context, store *db.PostgresStore) *auth.User {
	t.Helper()
	return testutil.SeedUser(t, ctx, store)
}

func TestPostgresSecurityGroups_CRUD(t *testing.T) {
	t.Parallel()
	repo, _ := newTestSGRepo(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

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
