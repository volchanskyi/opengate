package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/faulttest"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// faultEnv is an in-process server whose Devices port is a fault decorator set at wiring time.
type faultEnv struct {
	server  *httptest.Server
	devices *faulttest.FaultDevices
	token   string
}

func newFaultEnv(t *testing.T) *faultEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	devices := faulttest.WrapDevices(testutil.NewTestDevices(t, store))

	jwtCfg := &auth.JWTConfig{
		Secret:   "integration-test-secret-32-bytes!",
		Issuer:   "opengate-integration",
		Duration: 15 * time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := api.NewServer(api.ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        devices,
		Sites:          testutil.NewTestSites(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
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

	// A token in the seeded default tenant lets the real repository answer 404 for an unknown device.
	token, err := jwtCfg.GenerateToken(uuid.New(), "fault-admin@example.com", true, dbtx.DefaultTenantID)
	require.NoError(t, err)

	return &faultEnv{server: ts, devices: devices, token: token}
}

func (e *faultEnv) get(t *testing.T, path string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, e.server.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.server.Client().Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

const faultDevicePath = "/api/v1/devices/"

func TestFaultSuite_RepositoryError(t *testing.T) {
	t.Parallel()
	env := newFaultEnv(t)
	env.devices.Arm("Get", faulttest.Spec{Action: faulttest.ActionError})

	code := env.get(t, faultDevicePath+uuid.NewString())
	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestFaultSuite_PanicRecovery(t *testing.T) {
	t.Parallel()
	env := newFaultEnv(t)
	env.devices.Arm("Get", faulttest.Spec{Action: faulttest.ActionPanic, Once: true})

	panicked := env.get(t, faultDevicePath+uuid.NewString())
	assert.Equal(t, http.StatusInternalServerError, panicked, "panic must be recovered as 500")

	// The Once fault has cleared, so the next request reaches the real repository.
	survived := env.get(t, faultDevicePath+uuid.NewString())
	assert.Equal(t, http.StatusNotFound, survived, "the next request after a recovered panic must succeed normally")
}

func TestFaultSuite_DelayIsBoundedThenDelegates(t *testing.T) {
	t.Parallel()
	env := newFaultEnv(t)
	const delay = 150 * time.Millisecond
	env.devices.Arm("Get", faulttest.Spec{Action: faulttest.ActionDelay, Delay: delay})

	start := time.Now()
	code := env.get(t, faultDevicePath+uuid.NewString())
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusNotFound, code, "after the delay the real repository answers (unknown device → 404)")
	assert.GreaterOrEqual(t, elapsed, delay, "the response must be delayed by at least the injected delay")
}

func TestFaultSuite_Isolation(t *testing.T) {
	t.Parallel()
	env := newFaultEnv(t)
	env.devices.Arm("Get", faulttest.Spec{Action: faulttest.ActionError})

	var wg sync.WaitGroup
	var faultedCode, healthyCode int
	wg.Add(2)
	go func() {
		defer wg.Done()
		faultedCode = env.get(t, faultDevicePath+uuid.NewString())
	}()
	go func() {
		defer wg.Done()
		healthyCode = env.get(t, "/api/v1/devices")
	}()
	wg.Wait()

	assert.Equal(t, http.StatusInternalServerError, faultedCode, "the faulted Get path returns 500")
	assert.Equal(t, http.StatusOK, healthyCode, "the concurrent unfaulted list path is unaffected")
}
