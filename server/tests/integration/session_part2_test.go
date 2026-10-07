package integration

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

// serverAgentGetter adapts *agentapi.AgentServer to api.AgentGetter, turning a missing agent's
// typed-nil *AgentConn into an interface nil so handler nil checks fire.
type serverAgentGetter struct{ srv *agentapi.AgentServer }

func (g serverAgentGetter) GetAgent(deviceID uuid.UUID) api.AgentControl {
	ac := g.srv.GetAgent(deviceID)
	if ac == nil {
		return nil
	}
	return ac
}

func (g serverAgentGetter) ListConnectedAgents() []api.AgentControl {
	conns := g.srv.ListConnectedAgents()
	out := make([]api.AgentControl, 0, len(conns))
	for _, ac := range conns {
		out = append(out, ac)
	}
	return out
}

func (g serverAgentGetter) RefreshAlertRules(ctx context.Context, organizationID uuid.UUID) int {
	return g.srv.RefreshAlertRules(ctx, organizationID)
}

func (g serverAgentGetter) RefreshAlertRulesForTenant(ctx context.Context, tenantID uuid.UUID) int {
	return g.srv.RefreshAlertRulesForTenant(ctx, tenantID)
}

func newSessionTestEnv(t *testing.T) *sessionTestEnv {
	t.Helper()
	return newSessionTestEnvWithAPITimeout(t, 0)
}

// newSessionTestEnvWithAPITimeout sets the API request timeout; zero keeps the production default.
func newSessionTestEnvWithAPITimeout(t *testing.T, apiTimeout time.Duration) *sessionTestEnv {
	t.Helper()

	store := testutil.NewTestStore(t)
	deviceUpdates := testutil.NewTestDeviceUpdates(t, store)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	r := relay.NewRelay(slog.Default())
	logger := testLogger()
	agentSrv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: deviceUpdates,
		Relay:         r,
		Notifier:      &notifications.NoopNotifier{},
		Logger:        logger,
	})

	ctx, cancel := context.WithCancel(context.Background())

	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		agentSrv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	agentAddr := agentSrv.Addr()

	jwtCfg := &auth.JWTConfig{
		Secret:   "integration-test-secret-32-bytes!",
		Issuer:   "opengate-integration",
		Duration: 15 * time.Minute,
	}

	sigConfig := signaling.DefaultConfig()
	signingKeys, err := updater.LoadOrGenerateSigningKeys(t.TempDir())
	require.NoError(t, err)
	manifestStore := updater.NewManifestStore(t.TempDir())

	apiSrv := api.NewServer(api.ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  deviceUpdates,
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Sites:          testutil.NewTestSites(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            jwtCfg,
		Agents:         serverAgentGetter{srv: agentSrv},
		Relay:          r,
		Signaling:      sigConfig,
		Notifier:       &notifications.NoopNotifier{},
		Signing:        signingKeys,
		Manifests:      manifestStore,
		Logger:         logger,
		RequestTimeout: apiTimeout,
	})
	ts := httptest.NewServer(apiSrv)

	t.Cleanup(func() {
		ts.Close()
		cancel()
		select {
		case <-listenDone:
		case <-time.After(2 * time.Second):
			t.Log("agent QUIC server did not exit within 2s of cancel")
		}
	})

	return &sessionTestEnv{
		store:         store,
		devices:       testutil.NewTestDevices(t, store),
		deviceUpdates: deviceUpdates,
		certMgr:       cm,
		relay:         r,
		agentSrv:      agentSrv,
		agentAddr:     agentAddr,
		httpSrv:       ts,
		jwt:           jwtCfg,
		signing:       signingKeys,
		manifests:     manifestStore,
		cancel:        cancel,
	}
}
