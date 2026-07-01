package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// stubAMT is a test double for amt.Operator that always returns "not connected".
type stubAMT struct{}

func (s *stubAMT) PowerAction(_ context.Context, _ uuid.UUID, _ int) error {
	return amt.ErrDeviceNotConnected
}

func (s *stubAMT) QueryDeviceInfo(_ context.Context, _ uuid.UUID) (*wsman.DeviceInfo, error) {
	return nil, amt.ErrDeviceNotConnected
}

func (s *stubAMT) ConnectedDeviceCount() int { return 0 }

const (
	pathUsersMe = "/api/v1/users/me"
	pathGroups  = "/api/v1/groups"
	aliceEmail  = "alice@example.com"
	webServer01 = "web-server-01"
)

func defaultTenantContext() context.Context {
	return dbtx.WithDefaultTenant(context.Background(), false)
}

// testEnv holds a running test server and its dependencies.
type testEnv struct {
	server        *httptest.Server
	store         *db.PostgresStore
	devices       device.Repository
	deviceUpdates updater.DeviceUpdateRepository
	jwt           *auth.JWTConfig
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	deviceUpdates := testutil.NewTestDeviceUpdates(t, store)
	devicesRepo := testutil.NewTestDevices(t, store)
	groupsRepo := testutil.NewTestGroups(t, store)
	hardwareRepo := testutil.NewTestHardware(t, store)
	deviceLogsRepo := testutil.NewTestLogs(t, store)

	jwtCfg := &auth.JWTConfig{
		Secret:   "integration-test-secret-32-bytes!",
		Issuer:   "opengate-integration",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := api.NewServer(api.ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  deviceUpdates,
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        devicesRepo,
		Groups:         groupsRepo,
		Hardware:       hardwareRepo,
		DeviceLogs:     deviceLogsRepo,
		WebPush:        testutil.NewTestWebPush(t, store),
		AMTDevices:     testutil.NewTestAMTDevices(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            jwtCfg,
		AMT:            &stubAMT{},
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	})

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return &testEnv{server: ts, store: store, devices: devicesRepo, deviceUpdates: deviceUpdates, jwt: jwtCfg}
}

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
