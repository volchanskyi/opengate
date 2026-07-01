package auth_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"testing"
)

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
