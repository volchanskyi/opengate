package api

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"net/http"
	"testing"
)

func (env *deviceTestEnv) adminToken(t *testing.T) string {
	t.Helper()
	token, err := env.generateToken(env.user.ID, env.user.Email, true)
	require.NoError(t, err)
	return token
}

func TestGetDeviceLogs(t *testing.T) {
	t.Parallel()

	t.Run("non-admin is forbidden", func(t *testing.T) {
		env := setupDeviceTest(t, true)
		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs", env.ownerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Zero(t, env.agentStream.Len(), "denied caller must not reach the agent")
	})

	t.Run("admin device not found", func(t *testing.T) {
		env := setupDeviceTest(t, false)
		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String()+"/logs", env.adminToken(t), nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("admin but device offline", func(t *testing.T) {
		env := setupDeviceTest(t, false)
		w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs", env.adminToken(t), nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("requires auth", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String()+"/logs", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestLogFilterFromParams_MapsSourceAndUnit(t *testing.T) {
	t.Parallel()

	host := GetDeviceLogsParamsSource("host")
	unit := "nginx.service"
	level := GetDeviceLogsParamsLevel("WARN")
	got := logFilterFromParams(GetDeviceLogsParams{Level: &level, Source: &host, Unit: &unit})
	assert.Equal(t, "host", got.Source)
	assert.Equal(t, "nginx.service", got.Unit)
	assert.Equal(t, "WARN", got.Level)

	def := logFilterFromParams(GetDeviceLogsParams{})
	assert.Empty(t, def.Source, "omitted source defaults to the agent's own files")
	assert.Empty(t, def.Unit)
}

func TestGetDeviceLogs_OnlineWithoutCapability(t *testing.T) {
	t.Parallel()
	env := setupDeviceTest(t, true)
	ac := env.srv.agents.GetAgent(env.device.ID)
	require.NotNil(t, ac)
	// Clearing Capabilities simulates an agent that never advertised DeviceLogs.
	ac.(*agentapi.AgentConn).Capabilities = nil

	w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs", env.adminToken(t), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Zero(t, env.agentStream.Len())
}
