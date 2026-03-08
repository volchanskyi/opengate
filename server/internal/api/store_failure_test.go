package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

// TestHandlerStoreFailures verifies that every handler returns 500 when the store is unavailable.
func TestHandlerStoreFailures(t *testing.T) {
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)

	cfg := &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Seed a user for auth before closing the store.
	hash, err := auth.HashPassword("password123")
	require.NoError(t, err)
	userID := uuid.New()
	require.NoError(t, store.UpsertUser(t.Context(), &db.User{
		ID: userID, Email: "err@example.com", PasswordHash: hash,
	}))
	token, err := cfg.GenerateToken(userID, "err@example.com", true)
	require.NoError(t, err)

	srv := NewServer(store, cfg, &stubAgentGetter{}, relay.NewRelay(), nil, &notifications.NoopNotifier{}, logger)
	store.Close() // force all subsequent store calls to fail

	groupID := uuid.New()
	deviceID := uuid.New()

	tests := []struct {
		name   string
		method string
		path   string
		body   interface{}
		status int
	}{
		{"register store error", http.MethodPost, testPathRegister, map[string]string{"email": "x@x.com", "password": "pass"}, http.StatusInternalServerError},
		{"login store error", http.MethodPost, testPathLogin, map[string]string{"email": "err@example.com", "password": "password123"}, http.StatusInternalServerError},
		{"list devices store error", http.MethodGet, testPathDevices + "?group_id=" + groupID.String(), nil, http.StatusInternalServerError},
		{"get device store error", http.MethodGet, testPathDevicesS + deviceID.String(), nil, http.StatusInternalServerError},
		{"delete device store error", http.MethodDelete, testPathDevicesS + deviceID.String(), nil, http.StatusInternalServerError},
		{"create group store error", http.MethodPost, testPathGroups, map[string]string{"name": "g"}, http.StatusInternalServerError},
		{"list groups store error", http.MethodGet, testPathGroups, nil, http.StatusInternalServerError},
		{"get group store error", http.MethodGet, testPathGroupsS + groupID.String(), nil, http.StatusInternalServerError},
		{"delete group store error", http.MethodDelete, testPathGroupsS + groupID.String(), nil, http.StatusInternalServerError},
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
