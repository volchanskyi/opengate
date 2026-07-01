package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

// TestGroupIDOR verifies that users cannot access other users' groups.
func TestGroupIDOR(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	owner, ownerToken := seedTestUser(t, srv, cfg, "group-owner@example.com", false)
	_, attackerToken := seedTestUser(t, srv, cfg, "group-attacker@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "group-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "group-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store, owner.ID)

	t.Run("get group owner succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), ownerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get group attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("get group admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("delete group attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+group.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete group admin succeeds", func(t *testing.T) {
		g2 := testutil.SeedGroup(t, ctx, srv.store, owner.ID)
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+g2.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("list groups returns only own groups", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroups, attackerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var groups []Group
		json.NewDecoder(w.Body).Decode(&groups)
		assert.Empty(t, groups)
	})
}

// TestSessionIDOR verifies session ownership checks.
func TestSessionIDOR(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	owner, ownerToken := seedTestUser(t, srv, cfg, "sess-owner@example.com", false)
	_, attackerToken := seedTestUser(t, srv, cfg, "sess-attacker@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "sess-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "sess-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store, owner.ID)
	device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
	sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, owner.ID)

	t.Run("delete session owner succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess.Token, ownerToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("delete session attacker forbidden", func(t *testing.T) {
		sess2 := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, owner.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess2.Token, attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete session admin succeeds", func(t *testing.T) {
		sess3 := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, owner.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess3.Token, adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
