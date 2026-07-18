package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The concrete agent connection must satisfy the consumer-defined port, so the
// composition root can hand real *agentapi.AgentConn values to handlers that
// depend only on AgentControl.
var _ AgentControl = (*agentapi.AgentConn)(nil)

// fakeAgentControl is a hand-written AgentControl double: it records the last
// control-write and returns caller-configured results, so a handler test needs
// no real QUIC connection or agent read loop.
type fakeAgentControl struct {
	meta agentapi.AgentMeta

	restartCalls  int
	restartReason string
	restartErr    error

	maintenanceCalls   int
	maintenanceEnabled bool
	maintenanceErr     error

	logs      []device.LogEntry
	logsTotal int
	logsErr   error

	hwErr error
}

func (f *fakeAgentControl) SendSessionRequest(_ context.Context, _ protocol.SessionToken, _ string, _ protocol.Permissions) error {
	return nil
}

func (f *fakeAgentControl) SendAgentUpdate(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (f *fakeAgentControl) SendRestartAgent(_ context.Context, reason string) error {
	f.restartCalls++
	f.restartReason = reason
	return f.restartErr
}

func (f *fakeAgentControl) SendRequestHardwareReport(_ context.Context) error {
	return f.hwErr
}

func (f *fakeAgentControl) SendSetMaintenanceMode(_ context.Context, enabled bool) error {
	f.maintenanceCalls++
	f.maintenanceEnabled = enabled
	return f.maintenanceErr
}

func (f *fakeAgentControl) RequestLogsSync(_ context.Context, _ device.LogFilter) ([]device.LogEntry, int, error) {
	return f.logs, f.logsTotal, f.logsErr
}

func (f *fakeAgentControl) RequestLocalHistorySync(_ context.Context, _ string, _, _ int64, _ uint32) ([]protocol.HistoryPoint, bool, error) {
	return nil, false, nil
}

func (f *fakeAgentControl) Meta() agentapi.AgentMeta { return f.meta }

// controlTestEnv wires a server whose AgentGetter returns a fake AgentControl,
// proving the handlers depend only on the port, not the concrete conn.
type controlTestEnv struct {
	srv        *Server
	deviceID   protocol.DeviceID
	fake       *fakeAgentControl
	ownerToken string
	adminToken string
}

func setupControlTest(t *testing.T, fake *fakeAgentControl) *controlTestEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(t.Context(), true)

	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	dev := testutil.SeedDevice(t, ctx, store, group.ID)

	lookup := &stubAgentGetter{agents: map[protocol.DeviceID]AgentControl{dev.ID: fake}}
	srv, cfg := newTestServerWithStoreAndAgents(t, store, lookup, relay.NewRelay(slog.Default()))

	ownerToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.OrgID)
	require.NoError(t, err)
	adminToken, err := cfg.GenerateToken(user.ID, user.Email, true, user.OrgID)
	require.NoError(t, err)

	return &controlTestEnv{srv: srv, deviceID: dev.ID, fake: fake, ownerToken: ownerToken, adminToken: adminToken}
}

// TestAgentControl_RestartSendPath drives a control-write (Send*) through the
// handler backed only by a fake AgentControl, covering the positive (send
// succeeds → 200) and negative (send errors → 500) branches.
func TestAgentControl_RestartSendPath(t *testing.T) {
	t.Parallel()

	t.Run("send succeeds", func(t *testing.T) {
		t.Parallel()
		env := setupControlTest(t, &fakeAgentControl{})
		w := doRequest(env.srv, http.MethodPost, "/api/v1/devices/"+env.deviceID.String()+"/restart", env.ownerToken,
			map[string]string{"reason": "fake restart"})
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 1, env.fake.restartCalls)
		assert.Equal(t, "fake restart", env.fake.restartReason)
	})

	t.Run("send error surfaces as 500", func(t *testing.T) {
		t.Parallel()
		env := setupControlTest(t, &fakeAgentControl{restartErr: errors.New("stream closed")})
		w := doRequest(env.srv, http.MethodPost, "/api/v1/devices/"+env.deviceID.String()+"/restart", env.ownerToken,
			map[string]string{"reason": "fake restart"})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, 1, env.fake.restartCalls)
	})
}

// TestAgentControl_LogsRequestSyncPath drives a synchronous request/response read
// (Request*Sync) through the handler backed only by a fake AgentControl, covering
// the positive (entries returned → 200) and negative (capability error → 404)
// branches — proving the read surface is decoupled from the concrete conn too.
func TestAgentControl_LogsRequestSyncPath(t *testing.T) {
	t.Parallel()

	t.Run("entries returned", func(t *testing.T) {
		t.Parallel()
		env := setupControlTest(t, &fakeAgentControl{
			logs:      []device.LogEntry{{Level: "info", Message: "hello"}},
			logsTotal: 1,
		})
		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.deviceID.String()+"/logs", env.adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("capability error maps to 404", func(t *testing.T) {
		t.Parallel()
		env := setupControlTest(t, &fakeAgentControl{
			logsErr: fmt.Errorf("%w: %s", agentapi.ErrCapabilityNotAdvertised, protocol.CapDeviceLogs),
		})
		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.deviceID.String()+"/logs", env.adminToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestAgentControl_EligibleAgentsMetadataFilter pins that eligibleAgents reads
// os/arch/version through the AgentControl.Meta() port and applies the same
// filter as before: matching os+arch, excluding the already-current version.
func TestAgentControl_EligibleAgentsMetadataFilter(t *testing.T) {
	t.Parallel()

	match := &fakeAgentControl{meta: agentapi.AgentMeta{DeviceID: uuid.New(), OS: "linux", Arch: "amd64", AgentVersion: "1.0.0"}}
	wrongOS := &fakeAgentControl{meta: agentapi.AgentMeta{DeviceID: uuid.New(), OS: "windows", Arch: "amd64", AgentVersion: "1.0.0"}}
	wrongArch := &fakeAgentControl{meta: agentapi.AgentMeta{DeviceID: uuid.New(), OS: "linux", Arch: "arm64", AgentVersion: "1.0.0"}}
	alreadyCurrent := &fakeAgentControl{meta: agentapi.AgentMeta{DeviceID: uuid.New(), OS: "linux", Arch: "amd64", AgentVersion: "2.0.0"}}

	s := &Server{agents: &stubAgentGetter{agents: map[protocol.DeviceID]AgentControl{
		match.meta.DeviceID:          match,
		wrongOS.meta.DeviceID:        wrongOS,
		wrongArch.meta.DeviceID:      wrongArch,
		alreadyCurrent.meta.DeviceID: alreadyCurrent,
	}}}

	eligible := s.eligibleAgents("linux", "amd64", "2.0.0", nil)
	require.Len(t, eligible, 1)
	assert.Equal(t, match.meta.DeviceID, eligible[0].Meta().DeviceID)

	t.Run("target set narrows to the requested device ids", func(t *testing.T) {
		t.Parallel()
		none := s.eligibleAgents("linux", "amd64", "2.0.0", map[string]struct{}{uuid.New().String(): {}})
		assert.Empty(t, none)
	})
}
