package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// TestRegisterPromotesOnlyTheFirstUser pins the bootstrap-admin boundary.
func TestRegisterPromotesOnlyTheFirstUser(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := testTenantContext(t)

	firstEmail := "first-admin@example.com"
	first := doRequest(srv, http.MethodPost, testPathRegister, "", map[string]string{
		"email": firstEmail, "password": "password123",
	})
	require.Equal(t, http.StatusCreated, first.Code)
	firstUser, err := srv.users.GetByEmail(ctx, firstEmail)
	require.NoError(t, err)
	firstIsAdmin, err := srv.securityGroups.IsUserInGroup(ctx, firstUser.ID, auth.AdminGroupID)
	require.NoError(t, err)
	assert.True(t, firstIsAdmin)

	secondEmail := "second-regular@example.com"
	second := doRequest(srv, http.MethodPost, testPathRegister, "", map[string]string{
		"email": secondEmail, "password": "password123",
	})
	require.Equal(t, http.StatusCreated, second.Code)
	secondUser, err := srv.users.GetByEmail(ctx, secondEmail)
	require.NoError(t, err)
	secondIsAdmin, err := srv.securityGroups.IsUserInGroup(ctx, secondUser.ID, auth.AdminGroupID)
	require.NoError(t, err)
	assert.False(t, secondIsAdmin)
}
