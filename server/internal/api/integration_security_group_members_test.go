// Group membership: who is in the Administrators group, and the one member the
// installation may not lose.
package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const pathSecurityGroups = "/api/v1/security-groups"

func TestSecurityGroup_AdminCanListGroups(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodGet, pathSecurityGroups, adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var groups []auth.SecurityGroup
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
	require.GreaterOrEqual(t, len(groups), 1)

	// Should contain the Administrators group.
	found := false
	for _, g := range groups {
		if g.Name == "Administrators" {
			found = true
			assert.True(t, g.IsSystem)
		}
	}
	assert.True(t, found, "should contain Administrators group")
}

func TestSecurityGroup_AdminCanAddMember(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Register a regular user.
	regularToken := env.register(t, "regular-sg@example.com", "pass1234")
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
	var regUser db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
	resp.Body.Close()
	assert.False(t, regUser.IsAdmin)

	// Add regular user to Administrators group.
	resp = env.doJSON(t, http.MethodPost, pathSecurityGroups+"/"+auth.AdminGroupID.String()+"/members",
		adminToken, map[string]string{"user_id": regUser.ID.String()})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Re-login to get updated JWT.
	newToken := env.login(t, "regular-sg@example.com", "pass1234")

	// Now they can access admin endpoints.
	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/users", newToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestSecurityGroup_AdminCanRemoveMember(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	admin1, admin1Pass := testutil.SeedAdminUser(t, ctx, env.store)
	admin1Token := env.login(t, admin1.Email, admin1Pass)

	// Add a second admin.
	admin2, admin2Pass := testutil.SeedAdminUser(t, ctx, env.store)
	admin2Token := env.login(t, admin2.Email, admin2Pass)
	_ = admin2Token

	// Remove admin2 from Administrators group.
	resp := env.doJSON(t, http.MethodDelete,
		pathSecurityGroups+"/"+auth.AdminGroupID.String()+"/members/"+admin2.ID.String(),
		admin1Token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Re-login admin2 — should no longer have admin.
	newToken := env.login(t, admin2.Email, admin2Pass)
	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/users", newToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp2.StatusCode)
}
