package auth_test

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"testing"
)

func TestPostgresSecurityGroups_LastAdminProtection(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)

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
