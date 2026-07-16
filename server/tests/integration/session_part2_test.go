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

// serverAgentGetter bridges the concrete *agentapi.AgentServer to api.AgentGetter
// for this integration composition root — the same conversion main.go's
// production adapter performs, including turning a missing agent's typed-nil
// *AgentConn into an interface nil so handler `ac == nil` checks still fire.
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

func (g serverAgentGetter) DeregisterAgent(ctx context.Context, deviceID uuid.UUID) {
	g.srv.DeregisterAgent(ctx, deviceID)
}

func newSessionTestEnv(t *testing.T) *sessionTestEnv {
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
	agentAddr := agentSrv.Addr() // wait for QUIC to be ready

	jwtCfg := &auth.JWTConfig{
		Secret:   "integration-test-secret-32-bytes!",
		Issuer:   "opengate-integration",
		Duration: 15 * time.Minute,
	}

	sigTracker := signaling.NewTracker(signaling.DefaultConfig())
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
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		AMTDevices:     testutil.NewTestAMTDevices(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            jwtCfg,
		Agents:         serverAgentGetter{srv: agentSrv},
		Relay:          r,
		Signaling:      sigTracker,
		Notifier:       &notifications.NoopNotifier{},
		Signing:        signingKeys,
		Manifests:      manifestStore,
		Logger:         logger,
	})
	ts := httptest.NewServer(apiSrv)

	t.Cleanup(func() {
		ts.Close()
		cancel()
		// Wait for the QUIC server goroutine to exit instead of a blind sleep.
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
		sigTracker:    sigTracker,
		signing:       signingKeys,
		manifests:     manifestStore,
		cancel:        cancel,
	}
}
