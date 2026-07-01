package auth_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"testing"
)

func TestPostgresSecurityGroups_Members(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

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
