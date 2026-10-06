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
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

type stubAgentGetter struct {
	agents           map[protocol.DeviceID]AgentControl
	refreshedFor     []uuid.UUID
	refreshedTenants []uuid.UUID
}

func (s *stubAgentGetter) RefreshAlertRules(_ context.Context, organizationID uuid.UUID) int {
	s.refreshedFor = append(s.refreshedFor, organizationID)
	return len(s.agents)
}

func (s *stubAgentGetter) RefreshAlertRulesForTenant(_ context.Context, tenantID uuid.UUID) int {
	s.refreshedTenants = append(s.refreshedTenants, tenantID)
	return len(s.agents)
}

func (s *stubAgentGetter) GetAgent(deviceID protocol.DeviceID) AgentControl {
	if s == nil || s.agents == nil {
		return nil
	}
	ac, ok := s.agents[deviceID]
	if !ok {
		return nil
	}
	return ac
}

func (s *stubAgentGetter) ListConnectedAgents() []AgentControl {
	if s == nil || s.agents == nil {
		return nil
	}
	agents := make([]AgentControl, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	return agents
}

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

func testTenantContext(t *testing.T) context.Context {
	t.Helper()
	return dbtx.WithDefaultTenant(t.Context(), true)
}

// testPurger stands in for the lifecycle orchestrator and removes the device row when run.
type testPurger struct{ devices device.Repository }

func (p *testPurger) PurgeDevice(_ context.Context, tenantID, deviceID uuid.UUID, _ *uuid.UUID) (*lifecycle.PurgeJob, error) {
	return &lifecycle.PurgeJob{
		ID: uuid.New(), TenantID: tenantID, DeviceID: &deviceID,
		Scope: lifecycle.ScopeDevice, State: lifecycle.StateRequested,
	}, nil
}

func (p *testPurger) PurgeTenant(_ context.Context, tenantID uuid.UUID, _ *uuid.UUID) (*lifecycle.PurgeJob, error) {
	return &lifecycle.PurgeJob{
		ID: uuid.New(), TenantID: tenantID,
		Scope: lifecycle.ScopeTenant, State: lifecycle.StateRequested,
	}, nil
}

func (p *testPurger) Run(ctx context.Context, job *lifecycle.PurgeJob) error {
	if job.DeviceID == nil {
		return nil
	}
	return p.devices.Delete(ctx, *job.DeviceID)
}

func (p *testPurger) RunInBackground(*lifecycle.PurgeJob) {}

func newTestServer(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	devices := testutil.NewTestDevices(t, store)
	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        devices,
		Sites:          testutil.NewTestSites(t, store),
		Organizations:  testutil.NewTestOrganizations(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Purger:         &testPurger{devices: devices},
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	})
	return srv, cfg
}

func newTestServerWithStoreAndAgents(t *testing.T, store *db.PostgresStore, agents AgentGetter, r *relay.Relay) (*Server, *auth.JWTConfig) {
	t.Helper()
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	devices := testutil.NewTestDevices(t, store)
	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        devices,
		Sites:          testutil.NewTestSites(t, store),
		Organizations:  testutil.NewTestOrganizations(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         agents,
		AMT:            &stubAMTOperator{},
		Purger:         &testPurger{devices: devices},
		Relay:          r,
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	})
	return srv, cfg
}

func seedTestUser(t *testing.T, srv *Server, cfg *auth.JWTConfig, email string, isAdmin bool) (*auth.User, string) {
	t.Helper()
	hash, err := auth.HashPassword("password123")
	require.NoError(t, err)

	user := &auth.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  "Test User",
		IsAdmin:      isAdmin,
	}
	ctx := testTenantContext(t)
	err = srv.users.Upsert(ctx, user)
	require.NoError(t, err)

	token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.TenantID)
	require.NoError(t, err)

	return user, token
}

func doRequest(srv *Server, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	return doRequestWithHeaders(srv, method, path, token, body, nil)
}

func doRequestWithHeaders(srv *Server, method, path, token string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
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
