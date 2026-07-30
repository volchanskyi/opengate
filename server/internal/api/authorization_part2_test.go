package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestGroupReadsAreOrgWide verifies that the group list and group detail are
// fleet reads: every member of the organization sees every group in it.
func TestGroupReadsAreOrgWide(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	_, memberAToken := seedTestUser(t, srv, cfg, "group-member-a@example.com", false)
	_, memberBToken := seedTestUser(t, srv, cfg, "group-member-b@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "group-read-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "group-read-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store)

	for name, token := range map[string]string{
		"member a": memberAToken,
		"member b": memberBToken,
		"admin":    adminToken,
	} {
		t.Run("get group "+name, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), token, nil)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("list groups "+name, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, testPathGroups, token, nil)
			require.Equal(t, http.StatusOK, w.Code)
			var groups []Group
			require.NoError(t, json.NewDecoder(w.Body).Decode(&groups))
			assert.Len(t, groups, 1)
		})
	}
}

// TestGroupWritesAreAdminOnly verifies that creating and deleting a group are
// configuration changes behind the admin gate.
func TestGroupWritesAreAdminOnly(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	_, memberToken := seedTestUser(t, srv, cfg, "group-write-member@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "group-write-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "group-write-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	t.Run("create group member forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodPost, testPathGroups, memberToken, map[string]string{"name": "denied"})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("create group admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodPost, testPathGroups, adminToken, map[string]string{"name": "allowed"})
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("delete group member forbidden", func(t *testing.T) {
		g := testutil.SeedGroup(t, ctx, srv.store)
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+g.ID.String(), memberToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete group admin succeeds", func(t *testing.T) {
		g := testutil.SeedGroup(t, ctx, srv.store)
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+g.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// TestSessionCommandsAreOrgWide verifies that ending a session is a device
// command: any member of the organization may end any session on a device in
// that organization, and the session list is a plain fleet read.
func TestSessionCommandsAreOrgWide(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	creator, creatorToken := seedTestUser(t, srv, cfg, "sess-creator@example.com", false)
	_, peerToken := seedTestUser(t, srv, cfg, "sess-peer@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "sess-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "sess-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	t.Run("list sessions peer succeeds", func(t *testing.T) {
		testutil.SeedAgentSession(t, ctx, srv.store, dev.ID, creator.ID)
		w := doRequest(srv, http.MethodGet, "/api/v1/sessions?device_id="+dev.ID.String(), peerToken, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var sessions []AgentSession
		require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
		assert.NotEmpty(t, sessions)
	})

	t.Run("delete session creator succeeds", func(t *testing.T) {
		sess := testutil.SeedAgentSession(t, ctx, srv.store, dev.ID, creator.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess.Token, creatorToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("delete session peer succeeds", func(t *testing.T) {
		sess := testutil.SeedAgentSession(t, ctx, srv.store, dev.ID, creator.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess.Token, peerToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("delete session admin succeeds", func(t *testing.T) {
		sess := testutil.SeedAgentSession(t, ctx, srv.store, dev.ID, creator.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess.Token, adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
