package integration

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestGroupLifecycle(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	token1 := env.register(t, "user1@example.com", "pass1234")
	token2 := env.register(t, "user2@example.com", "pass4567")

	// User 1 creates two groups
	for _, name := range []string{"group-a", "group-b"} {
		resp := env.doJSON(t, http.MethodPost, pathGroups, token1, map[string]string{"name": name})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		resp.Body.Close()
	}

	// User 2 creates one group
	resp := env.doJSON(t, http.MethodPost, pathGroups, token2, map[string]string{"name": "group-c"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	t.Run("user1 sees only their own groups", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, pathGroups, token1, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var groups []*device.Group
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
		assert.Len(t, groups, 2)
	})

	t.Run("user2 sees only their own group", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, pathGroups, token2, nil)
		defer resp.Body.Close()

		var groups []*device.Group
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
		assert.Len(t, groups, 1)
		assert.Equal(t, "group-c", groups[0].Name)
	})
}

func TestAdminAuthorization(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// Create admin user first (so the DB is not empty when the regular user registers).
	adminUser, adminPass := testutil.SeedAdminUser(t, t.Context(), env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Create regular user via API (not the first user, so no bootstrap).
	regularToken := env.register(t, "regular@example.com", "pass1234")

	t.Run("admin can list all users", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/users", adminToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var users []*db.User
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&users))
		assert.GreaterOrEqual(t, len(users), 2)
	})

	t.Run("regular user cannot list users", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/users", regularToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("admin can delete a user", func(t *testing.T) {
		// Get regular user's ID
		resp := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
		var regUser db.User
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
		resp.Body.Close()

		resp = env.doJSON(t, http.MethodDelete, "/api/v1/users/"+regUser.ID.String(), adminToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Deleted user's token still validates (JWT is stateless) but /me returns 404
		resp2 := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
		defer resp2.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
	})

	t.Run("regular user cannot delete users", func(t *testing.T) {
		// Re-register a user since we deleted the previous one
		newToken := env.register(t, "new@example.com", "pass1234")
		resp := env.doJSON(t, http.MethodDelete, "/api/v1/users/"+adminUser.ID.String(), newToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}
