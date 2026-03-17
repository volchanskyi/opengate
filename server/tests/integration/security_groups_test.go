package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const pathSecurityGroups = "/api/v1/security-groups"

func TestSecurityGroup_AdminCanListGroups(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodGet, pathSecurityGroups, adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var groups []db.SecurityGroup
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
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Register a regular user.
	regularToken := env.register(t, "regular-sg@example.com", "pass123")
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
	var regUser db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
	resp.Body.Close()
	assert.False(t, regUser.IsAdmin)

	// Add regular user to Administrators group.
	resp = env.doJSON(t, http.MethodPost, pathSecurityGroups+"/"+db.AdminGroupID.String()+"/members",
		adminToken, map[string]string{"user_id": regUser.ID.String()})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Re-login to get updated JWT.
	newToken := env.login(t, "regular-sg@example.com", "pass123")

	// Now they can access admin endpoints.
	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/users", newToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestSecurityGroup_AdminCanRemoveMember(t *testing.T) {
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
		pathSecurityGroups+"/"+db.AdminGroupID.String()+"/members/"+admin2.ID.String(),
		admin1Token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Re-login admin2 — should no longer have admin.
	newToken := env.login(t, admin2.Email, admin2Pass)
	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/users", newToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp2.StatusCode)
}

func TestSecurityGroup_NonAdminBlocked(t *testing.T) {
	env := newTestEnv(t)

	// Seed an admin first so the registered user is NOT the first user.
	testutil.SeedAdminUser(t, t.Context(), env.store)
	regularToken := env.register(t, "nonadmin-sg@example.com", "pass123")

	endpoints := []struct {
		method string
		path   string
		expect int // POST with no body may 400 before auth check
	}{
		{http.MethodGet, pathSecurityGroups, http.StatusForbidden},
		{http.MethodGet, pathSecurityGroups + "/" + db.AdminGroupID.String(), http.StatusForbidden},
		{http.MethodDelete, pathSecurityGroups + "/" + db.AdminGroupID.String(), http.StatusForbidden},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			resp := env.doJSON(t, ep.method, ep.path, regularToken, nil)
			defer resp.Body.Close()
			assert.Equal(t, ep.expect, resp.StatusCode)
		})
	}

	// POST endpoints with valid bodies — non-admin should get 403.
	t.Run("POST "+pathSecurityGroups, func(t *testing.T) {
		resp := env.doJSON(t, http.MethodPost, pathSecurityGroups, regularToken,
			map[string]string{"name": "test-group"})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
	t.Run("POST "+pathSecurityGroups+"/members", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodPost,
			pathSecurityGroups+"/"+db.AdminGroupID.String()+"/members",
			regularToken, map[string]string{"user_id": "00000000-0000-0000-0000-000000000002"})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestSecurityGroup_CannotDeleteSystemGroup(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodDelete, pathSecurityGroups+"/"+db.AdminGroupID.String(), adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSecurityGroup_CannotRemoveLastAdmin(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	// Only one admin in the group.
	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodDelete,
		pathSecurityGroups+"/"+db.AdminGroupID.String()+"/members/"+adminUser.ID.String(),
		adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestSecurityGroup_AuditLogging(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Add a user to trigger audit event.
	regularToken := env.register(t, "audit-sg@example.com", "pass123")
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
	var regUser db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
	resp.Body.Close()

	resp = env.doJSON(t, http.MethodPost, pathSecurityGroups+"/"+db.AdminGroupID.String()+"/members",
		adminToken, map[string]string{"user_id": regUser.ID.String()})
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)

	// Check audit log for the add_member action.
	resp = env.doJSON(t, http.MethodGet, "/api/v1/audit?action=security_group.add_member", adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var events []db.AuditEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&events))
	assert.NotEmpty(t, events)
}

func TestAdminFirstUserBootstrap(t *testing.T) {
	env := newTestEnv(t)

	// First user registered becomes admin.
	firstToken := env.register(t, "first@example.com", "pass123")

	resp := env.doJSON(t, http.MethodGet, "/api/v1/users", firstToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "first registered user should be admin")
}

func TestAdminSecondUserNotAdmin(t *testing.T) {
	env := newTestEnv(t)

	// First user becomes admin.
	_ = env.register(t, "bootstrap-first@example.com", "pass123")

	// Second user should NOT be admin.
	secondToken := env.register(t, "bootstrap-second@example.com", "pass123")

	resp := env.doJSON(t, http.MethodGet, "/api/v1/users", secondToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "second registered user should not be admin")
}
