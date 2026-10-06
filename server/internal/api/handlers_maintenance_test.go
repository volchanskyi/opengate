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
	site  *device.Site
	dev   *device.Device
	fake  *fakeAgentControl
	token string
	ctx   context.Context
}

func setupMaintenanceEnv(t *testing.T, connected bool) *maintEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(t.Context(), true)
	owner := testutil.SeedUser(t, ctx, store)
	site := testutil.SeedSite(t, ctx, store)
	dev := testutil.SeedDevice(t, ctx, store, site.ID)

	fake := &fakeAgentControl{}
	agents := map[protocol.DeviceID]AgentControl{}
	if connected {
		agents[dev.ID] = fake
	}
	srv, cfg := newTestServerWithStoreAndAgents(t, store, &stubAgentGetter{agents: agents}, relay.NewRelay(slog.Default()))
	token, err := cfg.GenerateToken(owner.ID, owner.Email, owner.IsAdmin, owner.TenantID)
	require.NoError(t, err)

	return &maintEnv{srv: srv, cfg: cfg, store: store, owner: owner, site: site, dev: dev, fake: fake, token: token, ctx: ctx}
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

// maintenanceOn reports the optional maintenance_on flag, where absent means Active.
func maintenanceOn(d Device) bool {
	return d.MaintenanceOn != nil && *d.MaintenanceOn
}

func TestSetDeviceMaintenance_EnterAndExit(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)

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

	assert.Equal(t, 1, env.fake.maintenanceCalls)
	assert.True(t, env.fake.maintenanceEnabled)

	// auditLog runs in a goroutine, so the test polls until the event lands.
	var events []*audit.Event
	require.Eventually(t, func() bool {
		var err error
		events, err = env.srv.audit.Query(env.ctx, audit.Query{Action: "device.maintenance.enter", Limit: 10})
		return err == nil && len(events) == 1
	}, 2*time.Second, 25*time.Millisecond, "device.maintenance.enter audit event should be written")
	assert.Equal(t, env.dev.ID.String(), events[0].Target)

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

	w := doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, maintenanceOn(decodeDevice(t, w.Body)))
	assert.Equal(t, 1, env.fake.maintenanceCalls)
}

func TestSetDeviceMaintenance_OpenToTenantMembers(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)
	_, peerToken := seedTestUser(t, env.srv, env.cfg, "maintenance-peer@example.com", false)

	w := doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), peerToken,
		map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, maintenanceOn(decodeDevice(t, w.Body)))
	assert.Equal(t, 1, env.fake.maintenanceCalls)
}

func TestSetDeviceMaintenance_NotFound(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)

	w := doRequest(env.srv, http.MethodPost, maintenancePath(uuid.New()), env.token,
		map[string]any{"enabled": true})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestMaintenanceCountReachesTheSummary(t *testing.T) {
	t.Parallel()
	env := setupMaintenanceEnv(t, true)

	// The static summary route resolves ahead of /devices/{id}.
	w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/summary", env.token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var before DeviceSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &before))

	w = doRequest(env.srv, http.MethodPost, maintenancePath(env.dev.ID), env.token,
		map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code)

	w = doRequest(env.srv, http.MethodGet, "/api/v1/devices/summary", env.token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var after DeviceSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &after))
	assert.Equal(t, before.Maintenance+1, after.Maintenance)
	assert.Equal(t, before.Total, after.Total, "a maintenance toggle never changes the fleet size")
}
