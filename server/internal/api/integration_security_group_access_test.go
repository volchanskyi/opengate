// Who may change the Administrators group, and what may not be changed at all.
package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestSecurityGroup_NonAdminBlocked(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// Seed an admin first so the registered user is NOT the first user.
	testutil.SeedAdminUser(t, t.Context(), env.store)
	regularToken := env.register(t, "nonadmin-sg@example.com", "pass1234")

	endpoints := []struct {
		method string
		path   string
		expect int // POST with no body may 400 before auth check
	}{
		{http.MethodGet, pathSecurityGroups, http.StatusForbidden},
		{http.MethodGet, pathSecurityGroups + "/" + auth.AdminGroupID.String(), http.StatusForbidden},
		{http.MethodDelete, pathSecurityGroups + "/" + auth.AdminGroupID.String(), http.StatusForbidden},
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
			pathSecurityGroups+"/"+auth.AdminGroupID.String()+"/members",
			regularToken, map[string]string{"user_id": "00000000-0000-0000-0000-000000000002"})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestSecurityGroup_CannotDeleteSystemGroup(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodDelete, pathSecurityGroups+"/"+auth.AdminGroupID.String(), adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSecurityGroup_CannotRemoveLastAdmin(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	// Only one admin in the group.
	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodDelete,
		pathSecurityGroups+"/"+auth.AdminGroupID.String()+"/members/"+adminUser.ID.String(),
		adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}
