// Package integration contains cross-package integration tests for the
// OpenGate server. These tests wire together real instances of api, auth, db,
// and cert to verify end-to-end behaviour through actual HTTP requests.
package integration

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// testEnv holds a running test server and its dependencies.
type testEnv struct {
	server *httptest.Server
	store  db.Store
	jwt    *auth.JWTConfig
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	store := testutil.NewTestStore(t)

	jwtCfg := &auth.JWTConfig{
		Secret:   "integration-test-secret-32-bytes!",
		Issuer:   "opengate-integration",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := api.NewServer(store, jwtCfg, logger)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return &testEnv{server: ts, store: store, jwt: jwtCfg}
}

// helpers

type tokenResponse struct {
	Token string `json:"token"`
}

func (e *testEnv) doJSON(t *testing.T, method, path, token string, body interface{}) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, e.server.URL+path, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.server.Client().Do(req)
	require.NoError(t, err)
	return resp
}

func (e *testEnv) register(t *testing.T, email, password string) string {
	t.Helper()
	resp := e.doJSON(t, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"email":    email,
		"password": password,
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var tok tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	return tok.Token
}

func (e *testEnv) login(t *testing.T, email, password string) string {
	t.Helper()
	resp := e.doJSON(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email":    email,
		"password": password,
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var tok tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	return tok.Token
}

// --- Integration Tests ---

func TestAuthFlow(t *testing.T) {
	env := newTestEnv(t)

	t.Run("register then login then access protected endpoint", func(t *testing.T) {
		// 1. Register
		regToken := env.register(t, "alice@example.com", "strongpass")
		assert.NotEmpty(t, regToken)

		// 2. Login with same credentials
		loginToken := env.login(t, "alice@example.com", "strongpass")
		assert.NotEmpty(t, loginToken)

		// 3. Use token to access protected endpoint
		resp := env.doJSON(t, http.MethodGet, "/api/v1/users/me", loginToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var user db.User
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
		assert.Equal(t, "alice@example.com", user.Email)
		assert.Empty(t, user.PasswordHash) // json:"-" omits it
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		// Generate a token that's already expired
		expiredCfg := &auth.JWTConfig{
			Secret:   env.jwt.Secret,
			Issuer:   env.jwt.Issuer,
			Duration: -1 * time.Hour,
		}
		expiredToken, err := expiredCfg.GenerateToken(uuid.New(), "expired@example.com", false)
		require.NoError(t, err)

		resp := env.doJSON(t, http.MethodGet, "/api/v1/users/me", expiredToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("no token is rejected", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/users/me", "", nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("wrong password fails login", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email":    "alice@example.com",
			"password": "wrongpass",
		})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

func TestDeviceLifecycle(t *testing.T) {
	env := newTestEnv(t)
	token := env.register(t, "devops@example.com", "pass123")

	// Get current user to know the owner ID
	resp := env.doJSON(t, http.MethodGet, "/api/v1/users/me", token, nil)
	var user db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	resp.Body.Close()

	// Create a group via API
	resp = env.doJSON(t, http.MethodPost, "/api/v1/groups", token, map[string]string{"name": "prod-servers"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var group db.Group
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&group))
	resp.Body.Close()
	assert.Equal(t, "prod-servers", group.Name)

	// Seed a device directly into the store (agents register via agentapi, not REST)
	device := &db.Device{
		ID:       uuid.New(),
		GroupID:  group.ID,
		Hostname: "web-server-01",
		OS:       "linux",
		Status:   db.StatusOnline,
	}
	require.NoError(t, env.store.UpsertDevice(t.Context(), device))

	t.Run("list devices in group", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/devices?group_id="+group.ID.String(), token, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var devices []*db.Device
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&devices))
		require.Len(t, devices, 1)
		assert.Equal(t, "web-server-01", devices[0].Hostname)
	})

	t.Run("get single device", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/devices/"+device.ID.String(), token, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var d db.Device
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&d))
		assert.Equal(t, "web-server-01", d.Hostname)
		assert.Equal(t, "linux", d.OS)
	})

	t.Run("cascade delete group removes devices", func(t *testing.T) {
		// Delete the group
		resp := env.doJSON(t, http.MethodDelete, "/api/v1/groups/"+group.ID.String(), token, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Device should no longer be findable
		resp2 := env.doJSON(t, http.MethodGet, "/api/v1/devices/"+device.ID.String(), token, nil)
		defer resp2.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
	})
}

func TestGroupLifecycle(t *testing.T) {
	env := newTestEnv(t)
	token1 := env.register(t, "user1@example.com", "pass123")
	token2 := env.register(t, "user2@example.com", "pass456")

	// User 1 creates two groups
	for _, name := range []string{"group-a", "group-b"} {
		resp := env.doJSON(t, http.MethodPost, "/api/v1/groups", token1, map[string]string{"name": name})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		resp.Body.Close()
	}

	// User 2 creates one group
	resp := env.doJSON(t, http.MethodPost, "/api/v1/groups", token2, map[string]string{"name": "group-c"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	t.Run("user1 sees only their own groups", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/groups", token1, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var groups []*db.Group
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
		assert.Len(t, groups, 2)
	})

	t.Run("user2 sees only their own group", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/groups", token2, nil)
		defer resp.Body.Close()

		var groups []*db.Group
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
		assert.Len(t, groups, 1)
		assert.Equal(t, "group-c", groups[0].Name)
	})
}

func TestAdminAuthorization(t *testing.T) {
	env := newTestEnv(t)

	// Create regular user via API
	regularToken := env.register(t, "regular@example.com", "pass123")

	// Create admin user directly in store (no API for promoting to admin)
	adminHash, err := auth.HashPassword("adminpass")
	require.NoError(t, err)
	adminUser := &db.User{
		ID:           uuid.New(),
		Email:        "admin@example.com",
		PasswordHash: adminHash,
		DisplayName:  "Admin",
		IsAdmin:      true,
	}
	require.NoError(t, env.store.UpsertUser(t.Context(), adminUser))
	adminToken := env.login(t, "admin@example.com", "adminpass")

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
		resp := env.doJSON(t, http.MethodGet, "/api/v1/users/me", regularToken, nil)
		var regUser db.User
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
		resp.Body.Close()

		resp = env.doJSON(t, http.MethodDelete, "/api/v1/users/"+regUser.ID.String(), adminToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Deleted user's token still validates (JWT is stateless) but /me returns 404
		resp2 := env.doJSON(t, http.MethodGet, "/api/v1/users/me", regularToken, nil)
		defer resp2.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
	})

	t.Run("regular user cannot delete users", func(t *testing.T) {
		// Re-register a user since we deleted the previous one
		newToken := env.register(t, "new@example.com", "pass")
		resp := env.doJSON(t, http.MethodDelete, "/api/v1/users/"+adminUser.ID.String(), newToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestConcurrentRequests(t *testing.T) {
	env := newTestEnv(t)
	token := env.register(t, "concurrent@example.com", "pass123")

	// Create a group for device listing
	resp := env.doJSON(t, http.MethodPost, "/api/v1/groups", token, map[string]string{"name": "concurrent-group"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var group db.Group
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&group))
	resp.Body.Close()

	// Fire 20 concurrent requests across different endpoints
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var resp *http.Response
			switch i % 4 {
			case 0:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/health", "", nil)
			case 1:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/users/me", token, nil)
			case 2:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/groups", token, nil)
			case 3:
				resp = env.doJSON(t, http.MethodGet, "/api/v1/devices?group_id="+group.ID.String(), token, nil)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- http.ErrAbortHandler
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	assert.Empty(t, errors, "some concurrent requests failed")
}
