package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func TestUserHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@example.com", true)
	regularUser, regularToken := seedTestUser(t, srv, cfg, "regular@example.com", false)

	t.Run("get me", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/users/me", regularToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var u db.User
		json.NewDecoder(w.Body).Decode(&u)
		assert.Equal(t, "regular@example.com", u.Email)
		assert.Empty(t, u.PasswordHash) // json:"-" should omit
	})

	t.Run("list users as admin", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/users", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var users []*db.User
		json.NewDecoder(w.Body).Decode(&users)
		assert.GreaterOrEqual(t, len(users), 2)
	})

	t.Run("list users as regular user forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/users", regularToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete user as admin", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/users/"+regularUser.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("delete user as regular forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/users/"+uuid.New().String(), regularToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete user invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/users/not-a-uuid", adminToken, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get me after user deleted", func(t *testing.T) {
		tempUser, tempToken := seedTestUser(t, srv, cfg, "temp@example.com", false)
		require.NoError(t, srv.store.DeleteUser(t.Context(), tempUser.ID))
		w := doRequest(srv, http.MethodGet, "/api/v1/users/me", tempToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("update user toggle admin", func(t *testing.T) {
		target, _ := seedTestUser(t, srv, cfg, "toggle@example.com", false)
		isAdmin := true
		body := map[string]interface{}{"is_admin": isAdmin}
		w := doRequest(srv, http.MethodPatch, "/api/v1/users/"+target.ID.String(), adminToken, body)
		assert.Equal(t, http.StatusOK, w.Code)

		var u User
		json.NewDecoder(w.Body).Decode(&u)
		assert.True(t, u.IsAdmin)
	})

	t.Run("update user display name", func(t *testing.T) {
		target, _ := seedTestUser(t, srv, cfg, "rename@example.com", false)
		body := map[string]interface{}{"display_name": "New Name"}
		w := doRequest(srv, http.MethodPatch, "/api/v1/users/"+target.ID.String(), adminToken, body)
		assert.Equal(t, http.StatusOK, w.Code)

		var u User
		json.NewDecoder(w.Body).Decode(&u)
		assert.Equal(t, "New Name", u.DisplayName)
	})

	t.Run("update user as regular user forbidden", func(t *testing.T) {
		body := map[string]interface{}{"display_name": "Hacked"}
		w := doRequest(srv, http.MethodPatch, "/api/v1/users/"+regularUser.ID.String(), regularToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("update user not found", func(t *testing.T) {
		body := map[string]interface{}{"display_name": "Ghost"}
		w := doRequest(srv, http.MethodPatch, "/api/v1/users/"+uuid.New().String(), adminToken, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("update user without auth returns 401", func(t *testing.T) {
		body := map[string]interface{}{"display_name": "Anon"}
		w := doRequest(srv, http.MethodPatch, "/api/v1/users/"+regularUser.ID.String(), "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
