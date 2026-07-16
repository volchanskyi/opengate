package integration

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

// faultEnv is an in-process server whose Devices port is a fault decorator, so a
// test can arm a fault on a repository method and assert the HTTP outcome. The
// substitution happens at wiring time — the domain packages and the shipped
// binary are untouched.
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
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
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

	// An admin token scoped to the seeded default org, so the real repository
	// returns a clean not-found (404) for an unknown device on the healthy path.
	token, err := jwtCfg.GenerateToken(uuid.New(), "fault-admin@example.com", true, dbtx.DefaultOrgID)
	require.NoError(t, err)

	return &faultEnv{server: ts, devices: devices, token: token}
}

// get issues an authenticated GET and returns the status code.
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

// TestFaultSuite_RepositoryError asserts an injected boundary error on a
// repository read maps to a 500 (acceptance criterion 4).
func TestFaultSuite_RepositoryError(t *testing.T) {
	t.Parallel()
	env := newFaultEnv(t)
	env.devices.Arm("Get", faulttest.Spec{Action: faulttest.ActionError})

	code := env.get(t, faultDevicePath+uuid.NewString())
	assert.Equal(t, http.StatusInternalServerError, code)
}

// TestFaultSuite_PanicRecovery asserts a panic in a repository call is recovered
// as a 500 and the very next request on the same server succeeds normally
// (acceptance criterion 2).
func TestFaultSuite_PanicRecovery(t *testing.T) {
	t.Parallel()
	env := newFaultEnv(t)
	env.devices.Arm("Get", faulttest.Spec{Action: faulttest.ActionPanic, Once: true})

	panicked := env.get(t, faultDevicePath+uuid.NewString())
	assert.Equal(t, http.StatusInternalServerError, panicked, "panic must be recovered as 500")

	// The Once fault auto-cleared; the next request reaches the real repository,
	// which returns a normal 404 for the unknown device — proving the process
	// and the server survived the panic.
	survived := env.get(t, faultDevicePath+uuid.NewString())
	assert.Equal(t, http.StatusNotFound, survived, "the next request after a recovered panic must succeed normally")
}

// TestFaultSuite_DelayIsBoundedThenDelegates asserts a delay fault waits and then
// delegates to the real call (acceptance criterion 4).
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

// TestFaultSuite_Isolation asserts a fault armed on one call path does not affect
// a concurrent request on an unfaulted path within the same in-process server
// (acceptance criterion 6).
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
		// ListAll is unfaulted (only "Get" is armed) → healthy empty list.
		healthyCode = env.get(t, "/api/v1/devices")
	}()
	wg.Wait()

	assert.Equal(t, http.StatusInternalServerError, faultedCode, "the faulted Get path returns 500")
	assert.Equal(t, http.StatusOK, healthyCode, "the concurrent unfaulted list path is unaffected")
}
