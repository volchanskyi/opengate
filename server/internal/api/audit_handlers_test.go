package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func TestAuditHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	adminUser, adminToken := seedTestUser(t, srv, cfg, "audit-admin@example.com", true)
	_, regularToken := seedTestUser(t, srv, cfg, "audit-regular@example.com", false)

	// Seed some audit events
	for i := range 5 {
		err := srv.store.WriteAuditEvent(t.Context(), &db.AuditEvent{
			ID:        0,
			UserID:    adminUser.ID,
			Action:    "user.login",
			Target:    fmt.Sprintf("target-%d", i),
			Details:   "test detail",
			CreatedAt: time.Now(),
		})
		require.NoError(t, err)
	}

	t.Run("list audit events as admin", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/audit", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var events []AuditEvent
		err := json.NewDecoder(w.Body).Decode(&events)
		require.NoError(t, err)
		assert.Len(t, events, 5)
	})

	t.Run("list audit events as regular user forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/audit", regularToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("list audit events without auth returns 401", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/audit", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("filter by action", func(t *testing.T) {
		// Add an event with different action
		err := srv.store.WriteAuditEvent(t.Context(), &db.AuditEvent{
			ID:        0,
			UserID:    adminUser.ID,
			Action:    "user.delete",
			Target:    "deleted-user",
			Details:   "",
			CreatedAt: time.Now(),
		})
		require.NoError(t, err)

		w := doRequest(srv, http.MethodGet, "/api/v1/audit?action=user.delete", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var events []AuditEvent
		err = json.NewDecoder(w.Body).Decode(&events)
		require.NoError(t, err)
		assert.Len(t, events, 1)
		assert.Equal(t, "user.delete", events[0].Action)
	})

	t.Run("filter by user_id", func(t *testing.T) {
		otherUser, _ := seedTestUser(t, srv, cfg, "other-audit@example.com", false)
		err := srv.store.WriteAuditEvent(t.Context(), &db.AuditEvent{
			ID:        0,
			UserID:    otherUser.ID,
			Action:    "session.create",
			Target:    "device-1",
			Details:   "",
			CreatedAt: time.Now(),
		})
		require.NoError(t, err)

		w := doRequest(srv, http.MethodGet, "/api/v1/audit?user_id="+otherUser.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var events []AuditEvent
		err = json.NewDecoder(w.Body).Decode(&events)
		require.NoError(t, err)
		assert.Len(t, events, 1)
		assert.Equal(t, otherUser.ID, events[0].UserId)
	})

	t.Run("pagination with limit and offset", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/audit?limit=2&offset=0", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var events []AuditEvent
		err := json.NewDecoder(w.Body).Decode(&events)
		require.NoError(t, err)
		assert.Len(t, events, 2)
	})

	t.Run("empty result returns empty array not null", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/audit?action=nonexistent.action", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "[]\n", w.Body.String())
	})
}
