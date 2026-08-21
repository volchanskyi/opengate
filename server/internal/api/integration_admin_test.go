package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newAdminEnv brings up a test environment with an administrator already logged
// in, which is the starting point every test in this file needs.
func newAdminEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	env := newTestEnv(t)
	adminUser, adminPass := testutil.SeedAdminUser(t, t.Context(), env.store)
	return env, env.login(t, adminUser.Email, adminPass)
}

// pollAuditEvents reads the audit endpoint until accept is satisfied. Audit
// writes are asynchronous, so a single read races the flush.
func pollAuditEvents(t *testing.T, env *testEnv, token, query string, accept func([]audit.Event) bool) []audit.Event {
	t.Helper()
	var events []audit.Event
	require.Eventuallyf(t, func() bool {
		r := env.doJSON(t, http.MethodGet, query, token, nil)
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			return false
		}
		events = nil
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			return false
		}
		return accept(events)
	}, 3*time.Second, 50*time.Millisecond, "audit endpoint %s never returned an acceptable page", query)
	return events
}

func TestAdminUserPromotion(t *testing.T) {
	t.Parallel()
	env, adminToken := newAdminEnv(t)

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
	t.Parallel()
	env, adminToken := newAdminEnv(t)

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

	events := pollAuditEvents(t, env, adminToken, "/api/v1/audit?action=user.delete", func(es []audit.Event) bool {
		for _, e := range es {
			if e.Target == victim.ID.String() {
				return true
			}
		}
		return false
	})
	for _, e := range events {
		assert.Equal(t, "user.delete", e.Action)
	}
}

func TestAdminAuditLogFiltering(t *testing.T) {
	t.Parallel()
	env, adminToken := newAdminEnv(t)

	// Create some audit events by performing actions
	env.register(t, "audit-filter-1@example.com", "pass1234")
	env.register(t, "audit-filter-2@example.com", "pass1234")

	// Create site (triggers audit log entry)
	resp := env.doJSON(t, http.MethodPost, pathSites, adminToken, map[string]string{"name": "audit-test-site"})
	resp.Body.Close()

	// Every event the filter returns must match it.
	events := pollAuditEvents(t, env, adminToken, "/api/v1/audit?action=user.delete", func([]audit.Event) bool { return true })
	for _, e := range events {
		assert.Equal(t, "user.delete", e.Action)
	}
}

func TestAdminAuditLogPagination(t *testing.T) {
	t.Parallel()
	env, adminToken := newAdminEnv(t)

	// Enough audited actions to fill more than one page.
	for i := 0; i < 5; i++ {
		resp := env.doJSON(t, http.MethodPost, pathSites, adminToken,
			map[string]string{"name": fmt.Sprintf("page-site-%d", i)})
		resp.Body.Close()
	}

	events := pollAuditEvents(t, env, adminToken, "/api/v1/audit?limit=2", func(es []audit.Event) bool {
		return len(es) > 0
	})
	assert.LessOrEqual(t, len(events), 2)

	// Request with offset
	resp2 := env.doJSON(t, http.MethodGet, "/api/v1/audit?limit=2&offset=2", adminToken, nil)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
