package auth_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
)

func TestPostgresSecurityGroups_TenantDeny(t *testing.T) {
	t.Parallel()
	repo, store := newTestSGRepo(t)
	orgB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), orgB, false)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	userA := seedUser(t, ctxA, store)
	userB := seedUser(t, ctxB, store)
	groupA := &auth.SecurityGroup{ID: uuid.New(), Name: "TenantA-" + uuid.New().String()[:8]}
	groupB := &auth.SecurityGroup{ID: uuid.New(), Name: "TenantB-" + uuid.New().String()[:8]}
	require.NoError(t, repo.Create(ctxA, groupA))
	require.NoError(t, repo.Create(ctxB, groupB))
	require.NoError(t, repo.AddMember(ctxA, groupA.ID, userA.ID))
	require.NoError(t, repo.AddMember(ctxB, groupB.ID, userB.ID))

	_, err := repo.Get(ctxA, groupB.ID)
	assert.ErrorIs(t, err, auth.ErrSecurityGroupNotFound)

	groups, err := repo.List(ctxA)
	require.NoError(t, err)
	for _, group := range groups {
		assert.NotEqual(t, groupB.ID, group.ID)
	}

	members, err := repo.ListMembers(ctxA, groupB.ID)
	require.NoError(t, err)
	assert.Empty(t, members)
	inGroup, err := repo.IsUserInGroup(ctxA, userB.ID, groupB.ID)
	require.NoError(t, err)
	assert.False(t, inGroup)
	count, err := repo.CountMembers(ctxA, groupB.ID)
	require.NoError(t, err)
	assert.Zero(t, count)

	_, err = repo.List(context.Background())
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}
