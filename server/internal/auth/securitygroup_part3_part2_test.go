package auth_test

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
)

func TestPostgresSecurityGroups_SyncIsAdmin(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	u := seedUser(t, ctx, store)

	got, err := testutil.NewTestUsers(t, store).Get(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAdmin)

	require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u.ID))
	got, err = testutil.NewTestUsers(t, store).Get(ctx, u.ID)
	require.NoError(t, err)
	assert.True(t, got.IsAdmin)

	// A second admin keeps the first removal clear of ErrLastAdmin.
	u2 := seedUser(t, ctx, store)
	require.NoError(t, repo.AddMember(ctx, auth.AdminGroupID, u2.ID))

	require.NoError(t, repo.RemoveMember(ctx, auth.AdminGroupID, u.ID))
	got, err = testutil.NewTestUsers(t, store).Get(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAdmin)
}
