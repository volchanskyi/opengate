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

// TestGroupLifecycle exercises the group surface end to end under the current
// authorization model: creating a group is a configuration change behind the
// admin gate, and the resulting groups are visible to every member of the
// organization — including the member who created none of them.
func TestGroupLifecycle(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	adminUser, adminPass := testutil.SeedAdminUser(t, t.Context(), env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)
	memberToken := env.register(t, "member@example.com", "pass4567")

	for _, name := range []string{"group-a", "group-b", "group-c"} {
		resp := env.doJSON(t, http.MethodPost, pathGroups, adminToken, map[string]string{"name": name})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		resp.Body.Close()
	}

	t.Run("a member cannot create a group", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodPost, pathGroups, memberToken, map[string]string{"name": "group-denied"})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	names := func(t *testing.T, token string) []string {
		t.Helper()
		resp := env.doJSON(t, http.MethodGet, pathGroups, token, nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var groups []*device.Group
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
		out := make([]string, 0, len(groups))
		for _, g := range groups {
			out = append(out, g.Name)
		}
		return out
	}

	t.Run("every member sees every group in the organization", func(t *testing.T) {
		want := []string{"group-a", "group-b", "group-c"}
		assert.Equal(t, want, names(t, adminToken))
		assert.Equal(t, want, names(t, memberToken), "the member created none of these and still sees them all")
	})

	t.Run("a member cannot delete a group", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, pathGroups, adminToken, nil)
		var groups []*device.Group
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
		resp.Body.Close()
		require.NotEmpty(t, groups)

		denied := env.doJSON(t, http.MethodDelete, pathGroups+"/"+groups[0].ID.String(), memberToken, nil)
		defer denied.Body.Close()
		assert.Equal(t, http.StatusForbidden, denied.StatusCode)
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
