package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

type auditHandlerEnv struct {
	srv          *Server
	cfg          *auth.JWTConfig
	adminUser    *auth.User
	adminToken   string
	regularToken string
}

func newAuditHandlerEnv(t *testing.T) auditHandlerEnv {
	t.Helper()
	srv, cfg := newTestServer(t)
	adminUser, adminToken := seedTestUser(t, srv, cfg, "audit-admin@example.com", true)
	_, regularToken := seedTestUser(t, srv, cfg, "audit-regular@example.com", false)
	for i := range 5 {
		writeAuditEvent(t, srv, adminUser.ID, "user.login", fmt.Sprintf("target-%d", i))
	}
	return auditHandlerEnv{srv: srv, cfg: cfg, adminUser: adminUser, adminToken: adminToken, regularToken: regularToken}
}

func writeAuditEvent(t *testing.T, srv *Server, userID auth.UserID, action, target string) {
	t.Helper()
	err := srv.audit.Write(testTenantContext(t), &audit.Event{
		UserID:    userID,
		Action:    action,
		Target:    target,
		Details:   "test detail",
		CreatedAt: time.Now(),
	})
	require.NoError(t, err)
}

func readAuditEvents(t *testing.T, srv *Server, token, query string) []AuditEvent {
	t.Helper()
	w := doRequest(srv, http.MethodGet, "/api/v1/audit"+query, token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var events []AuditEvent
	require.NoError(t, json.NewDecoder(w.Body).Decode(&events))
	return events
}

func TestAuditHandlersListAsAdmin(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	assert.Len(t, readAuditEvents(t, env.srv, env.adminToken, ""), 5)
}

func TestAuditHandlersRejectRegularUser(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	w := doRequest(env.srv, http.MethodGet, "/api/v1/audit", env.regularToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAuditHandlersRequireAuth(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	w := doRequest(env.srv, http.MethodGet, "/api/v1/audit", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuditHandlersFilterByAction(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	writeAuditEvent(t, env.srv, env.adminUser.ID, "user.delete", "deleted-user")
	events := readAuditEvents(t, env.srv, env.adminToken, "?action=user.delete")
	require.Len(t, events, 1)
	assert.Equal(t, "user.delete", events[0].Action)
}

func TestAuditHandlersFilterByUserID(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	otherUser, _ := seedTestUser(t, env.srv, env.cfg, "other-audit@example.com", false)
	writeAuditEvent(t, env.srv, otherUser.ID, "session.create", "device-1")
	events := readAuditEvents(t, env.srv, env.adminToken, "?user_id="+otherUser.ID.String())
	require.Len(t, events, 1)
	assert.Equal(t, otherUser.ID, events[0].UserId)
}

func TestAuditHandlersPagination(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	assert.Len(t, readAuditEvents(t, env.srv, env.adminToken, "?limit=2&offset=0"), 2)
}

func TestAuditHandlersEmptyResultIsArray(t *testing.T) {
	t.Parallel()
	env := newAuditHandlerEnv(t)
	w := doRequest(env.srv, http.MethodGet, "/api/v1/audit?action=nonexistent.action", env.adminToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]\n", w.Body.String())
}
