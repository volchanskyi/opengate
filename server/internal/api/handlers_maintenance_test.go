package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

type maintEnv struct {
	srv   *Server
	cfg   *auth.JWTConfig
	store *db.PostgresStore
	owner *auth.User
	group *device.Group
	dev   *device.Device
	fake  *fakeAgentControl
	token string
	ctx   context.Context
}

// setupMaintenanceEnv wires a server with one owned device. When connected is
// true the device's agent is in the lookup (so a push is attempted); when false
// the device is offline, exercising the "maintenance is a desired state, not a
// live command" path where the toggle still succeeds.
func setupMaintenanceEnv(t *testing.T, connected bool) *maintEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(t.Context(), true)
	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)
	dev := testutil.SeedDevice(t, ctx, store, group.ID)

	fake := &fakeAgentControl{}
	agents := map[protocol.DeviceID]AgentControl{}
	if connected {
		agents[dev.ID] = fake
	}
	srv, cfg := newTestServerWithStoreAndAgents(t, store, &stubAgentGetter{agents: agents}, relay.NewRelay(slog.Default()))
	token, err := cfg.GenerateToken(owner.ID, owner.Email, owner.IsAdmin, owner.OrgID)
	require.NoError(t, err)

	return &maintEnv{srv: srv, cfg: cfg, store: store, owner: owner, group: group, dev: dev, fake: fake, token: token, ctx: ctx}
}

func maintenancePath(id uuid.UUID) string {
	return "/api/v1/devices/" + id.String() + "/maintenance"
}

func decodeDevice(t *testing.T, w interface{ Bytes() []byte }) Device {
	t.Helper()
	var d Device
	require.NoError(t, json.Unmarshal(w.Bytes(), &d))
	return d
}

// maintenanceOn reads the optional maintenance_on flag: absent (nil) means the
// device is Active.
func maintenanceOn(d Device) bool {
	return d.MaintenanceOn != nil && *d.MaintenanceOn
}

func TestSetDeviceMaintenance_EnterAndExit(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)

	// Enter maintenance.
	w := doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": true, "reason": "kernel upgrade"})
	require.Equal(t, http.StatusOK, w.Code)

	d := decodeDevice(t, w.Body)
	assert.True(t, maintenanceOn(d))
	require.NotNil(t, d.MaintenanceReason)
	assert.Equal(t, "kernel upgrade", *d.MaintenanceReason)
	require.NotNil(t, d.MaintenanceSince)
	require.NotNil(t, d.MaintenanceBy)
	assert.Equal(t, env.owner.ID, *d.MaintenanceBy)

	// The desired state was pushed to the connected agent.
	assert.Equal(t, 1, env.fake.maintenanceCalls)
	assert.True(t, env.fake.maintenanceEnabled)

	// The enter was audited against the device. auditLog is async
	// (fire-and-forget goroutine) — poll until it lands.
	var events []*audit.Event
	require.Eventually(t, func() bool {
		var err error
		events, err = env.srv.audit.Query(env.ctx, audit.Query{Action: "device.maintenance.enter", Limit: 10})
		return err == nil && len(events) == 1
	}, 2*time.Second, 25*time.Millisecond, "device.maintenance.enter audit event should be written")
	assert.Equal(t, env.dev.ID.String(), events[0].Target)

	// Exit maintenance clears the fields and pushes false.
	w = doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, w.Code)

	d = decodeDevice(t, w.Body)
	assert.False(t, maintenanceOn(d))
	assert.Nil(t, d.MaintenanceSince)
	assert.Nil(t, d.MaintenanceBy)
	assert.Equal(t, 2, env.fake.maintenanceCalls)
	assert.False(t, env.fake.maintenanceEnabled)
}

func TestSetDeviceMaintenance_OfflineDeviceSucceeds(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, false)

	// No connected agent: the toggle is a desired state, so it must succeed
	// (persist + reconcile on reconnect) rather than 409 like RestartDevice.
	w := doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": true, "reason": "offline reboot"})
	require.Equal(t, http.StatusOK, w.Code)

	d := decodeDevice(t, w.Body)
	assert.True(t, maintenanceOn(d))
	assert.Equal(t, 0, env.fake.maintenanceCalls, "no push attempted for an offline device")
}

func TestSetDeviceMaintenance_PushFailureIsNonFatal(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)
	env.fake.maintenanceErr = errors.New("stream closed")

	// A failed push must not roll back the persisted desired state.
	w := doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, maintenanceOn(decodeDevice(t, w.Body)))
	assert.Equal(t, 1, env.fake.maintenanceCalls)
}

func TestSetDeviceMaintenance_Forbidden(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)
	_, otherToken := seedTestUser(t, env.srv, env.cfg, "not-owner@example.com", false)

	w := doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), otherToken,
		map[string]any{"enabled": true})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, env.fake.maintenanceCalls)
}

func TestSetDeviceMaintenance_NotFound(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)

	w := doRequest(env.srv, http.MethodPost, maintenancePath(uuid.New()), env.token,
		map[string]any{"enabled": true})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetDeviceMaintenanceSummary(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)

	// Static route must resolve ahead of /devices/{id}.
	w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/maintenance-summary", env.token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var before MaintenanceSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &before))

	w = doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)

	w = doRequest(env.srv, http.MethodGet, "/api/v1/devices/maintenance-summary", env.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var after MaintenanceSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &after))
	assert.Equal(t, before.Count+1, after.Count)
}
