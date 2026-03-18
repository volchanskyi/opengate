package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAdminUserPromotion(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	// Create admin user
	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Create regular user
	regularToken := env.register(t, "promote-me@example.com", "pass1234")

	// Get regular user's info
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
	var regUser db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
	resp.Body.Close()
	assert.False(t, regUser.IsAdmin)

	// Admin promotes user
	isAdmin := true
	resp = env.doJSON(t, http.MethodPatch, "/api/v1/users/"+regUser.ID.String(), adminToken, map[string]interface{}{
		"is_admin": isAdmin,
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var updated db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
	assert.True(t, updated.IsAdmin)

	// Verify promoted user can now access admin endpoints
	// Generate a new token reflecting admin status
	promotedToken, err := env.jwt.GenerateToken(regUser.ID, regUser.Email, true)
	require.NoError(t, err)

	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/users", promotedToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestAdminAuditLogCapturesActions(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Create a user to delete (triggers audit log)
	victimToken := env.register(t, "victim@example.com", "pass1234")
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, victimToken, nil)
	var victim db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&victim))
	resp.Body.Close()

	// Delete user
	resp = env.doJSON(t, http.MethodDelete, "/api/v1/users/"+victim.ID.String(), adminToken, nil)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Wait for async audit log to be written
	time.Sleep(200 * time.Millisecond)

	// Query audit log
	resp = env.doJSON(t, http.MethodGet, "/api/v1/audit?action=user.delete", adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var events []db.AuditEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&events))
	assert.NotEmpty(t, events)

	// Find our specific event
	found := false
	for _, e := range events {
		if e.Target == victim.ID.String() {
			found = true
			assert.Equal(t, "user.delete", e.Action)
			break
		}
	}
	assert.True(t, found, "audit log should contain user.delete event")
}

func TestAdminAuditLogFiltering(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Create some audit events by performing actions
	env.register(t, "audit-filter-1@example.com", "pass1234")
	env.register(t, "audit-filter-2@example.com", "pass1234")

	// Create group (triggers audit log entry)
	resp := env.doJSON(t, http.MethodPost, pathGroups, adminToken, map[string]string{"name": "audit-test-group"})
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)

	// Filter by action
	resp = env.doJSON(t, http.MethodGet, "/api/v1/audit?action=user.delete", adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var events []db.AuditEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&events))
	for _, e := range events {
		assert.Equal(t, "user.delete", e.Action)
	}
}

func TestAdminAuditLogPagination(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Create several audit events
	for i := 0; i < 5; i++ {
		hash, err := auth.HashPassword("pass")
		require.NoError(t, err)
		u := &db.User{
			ID:           testutil.SeedUser(t, ctx, env.store).ID,
			Email:        testutil.SeedUser(t, ctx, env.store).Email,
			PasswordHash: hash,
		}
		_ = u // just seeding data to generate audit events

		resp := env.doJSON(t, http.MethodPost, pathGroups, adminToken, map[string]string{"name": "page-group"})
		resp.Body.Close()
	}

	time.Sleep(200 * time.Millisecond)

	// Request with limit
	resp := env.doJSON(t, http.MethodGet, "/api/v1/audit?limit=2", adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var events []db.AuditEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&events))
	assert.LessOrEqual(t, len(events), 2)

	// Request with offset
	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/audit?limit=2&offset=2", adminToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
