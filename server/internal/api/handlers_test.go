package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func newTestServer(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(store, cfg, logger)
	return srv, cfg
}

// seedTestUser creates a user in the DB and returns the user + auth token.
func seedTestUser(t *testing.T, srv *Server, cfg *auth.JWTConfig, email string, isAdmin bool) (*db.User, string) {
	t.Helper()
	hash, err := auth.HashPassword("password123")
	require.NoError(t, err)

	user := &db.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  "Test User",
		IsAdmin:      isAdmin,
	}
	err = srv.store.UpsertUser(t.Context(), user)
	require.NoError(t, err)

	token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	return user, token
}

func doRequest(srv *Server, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func doRawRequest(srv *Server, method, path, token string, rawBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(rawBody))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// --- Health ---

func TestHandleHealth(t *testing.T) {
	srv, _ := newTestServer(t)

	t.Run("returns ok", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/health", "", nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "ok", resp["status"])
	})

	t.Run("returns 503 when db closed", func(t *testing.T) {
		// Create a separate server with a store we can close
		dir := t.TempDir()
		store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
		require.NoError(t, err)
		cfg := &auth.JWTConfig{Secret: "test-secret-key-at-least-32-bytes!", Issuer: "test", Duration: 15 * time.Minute}
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		closedSrv := NewServer(store, cfg, logger)
		store.Close()

		w := doRequest(closedSrv, http.MethodGet, "/api/v1/health", "", nil)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

// --- Auth ---

func TestHandleRegister(t *testing.T) {
	srv, _ := newTestServer(t)

	t.Run("successful registration", func(t *testing.T) {
		body := map[string]string{
			"email":        "new@example.com",
			"password":     "secret123",
			"display_name": "New User",
		}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var resp tokenResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotEmpty(t, resp.Token)
	})

	t.Run("missing email", func(t *testing.T) {
		body := map[string]string{"password": "secret"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing password", func(t *testing.T) {
		body := map[string]string{"email": "x@example.com"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, "/api/v1/auth/register", "", "not-json{{{")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleLogin(t *testing.T) {
	srv, cfg := newTestServer(t)
	seedTestUser(t, srv, cfg, "login@example.com", false)

	t.Run("successful login", func(t *testing.T) {
		body := map[string]string{"email": "login@example.com", "password": "password123"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp tokenResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotEmpty(t, resp.Token)
	})

	t.Run("wrong password", func(t *testing.T) {
		body := map[string]string{"email": "login@example.com", "password": "wrong"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unknown email", func(t *testing.T) {
		body := map[string]string{"email": "nobody@example.com", "password": "pass"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, "/api/v1/auth/login", "", "bad json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing fields", func(t *testing.T) {
		body := map[string]string{"email": "login@example.com"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// --- Devices ---

func TestHandleDevices(t *testing.T) {
	srv, cfg := newTestServer(t)
	user, token := seedTestUser(t, srv, cfg, "dev@example.com", false)

	// Create a group and device for testing
	group := &db.Group{ID: uuid.New(), Name: "test-group", OwnerID: user.ID}
	require.NoError(t, srv.store.CreateGroup(t.Context(), group))

	device := &db.Device{
		ID:       uuid.New(),
		GroupID:  group.ID,
		Hostname: "test-host",
		OS:       "linux",
		Status:   db.StatusOnline,
	}
	require.NoError(t, srv.store.UpsertDevice(t.Context(), device))

	t.Run("list devices", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices?group_id="+group.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var devices []*db.Device
		json.NewDecoder(w.Body).Decode(&devices)
		assert.Len(t, devices, 1)
		assert.Equal(t, device.ID, devices[0].ID)
	})

	t.Run("list devices missing group_id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get device", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+device.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var d db.Device
		json.NewDecoder(w.Body).Decode(&d)
		assert.Equal(t, device.Hostname, d.Hostname)
	})

	t.Run("get device not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete device", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/devices/"+device.ID.String(), token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("list devices invalid group_id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices?group_id=not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get device invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete device invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/devices/not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("requires auth", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/devices?group_id="+group.ID.String(), "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// --- Groups ---

func TestHandleGroups(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "grp@example.com", false)

	var createdGroupID uuid.UUID

	t.Run("create group", func(t *testing.T) {
		body := map[string]string{"name": "my-group"}
		w := doRequest(srv, http.MethodPost, "/api/v1/groups", token, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var g db.Group
		json.NewDecoder(w.Body).Decode(&g)
		assert.Equal(t, "my-group", g.Name)
		createdGroupID = g.ID
	})

	t.Run("list groups", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/groups", token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var groups []*db.Group
		json.NewDecoder(w.Body).Decode(&groups)
		assert.Len(t, groups, 1)
	})

	t.Run("get group", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/groups/"+createdGroupID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get group not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/groups/"+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("create group missing name", func(t *testing.T) {
		body := map[string]string{}
		w := doRequest(srv, http.MethodPost, "/api/v1/groups", token, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("create group invalid json", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, "/api/v1/groups", token, "bad json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get group invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/groups/not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete group invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/groups/not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete group", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/groups/"+createdGroupID.String(), token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// --- Users ---

func TestHandleUsers(t *testing.T) {
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
		// Create a temp user, get token, delete user, then try /me
		tempUser, tempToken := seedTestUser(t, srv, cfg, "temp@example.com", false)
		require.NoError(t, srv.store.DeleteUser(t.Context(), tempUser.ID))
		w := doRequest(srv, http.MethodGet, "/api/v1/users/me", tempToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestStoreErrorPaths verifies that handlers return 500 when the store is broken.
func TestStoreErrorPaths(t *testing.T) {
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)

	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Seed a user for auth before closing the store
	hash, err := auth.HashPassword("password123")
	require.NoError(t, err)
	userID := uuid.New()
	require.NoError(t, store.UpsertUser(t.Context(), &db.User{
		ID: userID, Email: "err@example.com", PasswordHash: hash,
	}))
	token, err := cfg.GenerateToken(userID, "err@example.com", true)
	require.NoError(t, err)

	srv := NewServer(store, cfg, logger)
	store.Close() // force all store calls to fail

	groupID := uuid.New()
	deviceID := uuid.New()

	tests := []struct {
		name   string
		method string
		path   string
		body   interface{}
		status int
	}{
		{"register store error", http.MethodPost, "/api/v1/auth/register", map[string]string{"email": "x@x.com", "password": "pass"}, http.StatusInternalServerError},
		{"login store error", http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "err@example.com", "password": "password123"}, http.StatusInternalServerError},
		{"list devices store error", http.MethodGet, "/api/v1/devices?group_id=" + groupID.String(), nil, http.StatusInternalServerError},
		{"get device store error", http.MethodGet, "/api/v1/devices/" + deviceID.String(), nil, http.StatusInternalServerError},
		{"delete device store error", http.MethodDelete, "/api/v1/devices/" + deviceID.String(), nil, http.StatusInternalServerError},
		{"create group store error", http.MethodPost, "/api/v1/groups", map[string]string{"name": "g"}, http.StatusInternalServerError},
		{"list groups store error", http.MethodGet, "/api/v1/groups", nil, http.StatusInternalServerError},
		{"get group store error", http.MethodGet, "/api/v1/groups/" + groupID.String(), nil, http.StatusInternalServerError},
		{"delete group store error", http.MethodDelete, "/api/v1/groups/" + groupID.String(), nil, http.StatusInternalServerError},
		{"list users store error", http.MethodGet, "/api/v1/users", nil, http.StatusInternalServerError},
		{"get me store error", http.MethodGet, "/api/v1/users/me", nil, http.StatusInternalServerError},
		{"delete user store error", http.MethodDelete, "/api/v1/users/" + userID.String(), nil, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(srv, tt.method, tt.path, token, tt.body)
			assert.Equal(t, tt.status, w.Code)
		})
	}
}
