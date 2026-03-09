package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/mps/wsman"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// stubAgentGetter is a test double for AgentGetter.
type stubAgentGetter struct {
	agents map[protocol.DeviceID]*agentapi.AgentConn
}

func (s *stubAgentGetter) GetAgent(deviceID protocol.DeviceID) *agentapi.AgentConn {
	if s == nil || s.agents == nil {
		return nil
	}
	return s.agents[deviceID]
}

// stubAMTOperator is a test double for AMTOperator.
type stubAMTOperator struct{}

func (s *stubAMTOperator) PowerAction(_ context.Context, _ uuid.UUID, _ int) error {
	return amt.ErrDeviceNotConnected
}

func (s *stubAMTOperator) QueryDeviceInfo(_ context.Context, _ uuid.UUID) (*wsman.DeviceInfo, error) {
	return nil, amt.ErrDeviceNotConnected
}

func (s *stubAMTOperator) ConnectedDeviceCount() int {
	return 0
}

func testJWTConfig() *auth.JWTConfig {
	return &auth.JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
}

// newTestServer creates a Server backed by a temporary SQLite store and a test JWTConfig.
func newTestServer(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:    store,
		JWT:      cfg,
		Agents:   &stubAgentGetter{},
		AMT:      &stubAMTOperator{},
		Relay:    relay.NewRelay(),
		Notifier: &notifications.NoopNotifier{},
		Logger:   logger,
	})
	return srv, cfg
}

// newTestServerWithAgents creates a Server with a custom AgentGetter and relay.
func newTestServerWithAgents(t *testing.T, agents AgentGetter, r *relay.Relay) (*Server, *auth.JWTConfig) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:    store,
		JWT:      cfg,
		Agents:   agents,
		AMT:      &stubAMTOperator{},
		Relay:    r,
		Notifier: &notifications.NoopNotifier{},
		Logger:   logger,
	})
	return srv, cfg
}

// seedTestUser inserts a user directly into the server's store and returns the user and a valid JWT.
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

// doRequest sends a JSON request to srv and returns the response recorder.
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

// doRawRequest sends a request with a raw string body to srv.
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
