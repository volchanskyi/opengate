package api

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"net/http"
	"testing"
)

// adminToken mints an administrator token for the env's seeded user; raw-log
// pulls are gated on admin.
func (env *deviceTestEnv) adminToken(t *testing.T) string {
	t.Helper()
	token, err := env.generateToken(env.user.ID, env.user.Email, true)
	require.NoError(t, err)
	return token
}

// TestGetDeviceLogs covers the deterministic gating paths of the raw-log broker.
// The happy-path round trip (agent responds, redaction, audit) is exercised by
// the integration test, since it needs a running agent read loop.
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

// TestGetDeviceLogs_OnlineWithoutCapability pins that an online agent that never
// advertised DeviceLogs is treated as unavailable rather than sent a request.
func TestGetDeviceLogs_OnlineWithoutCapability(t *testing.T) {
	t.Parallel()
	env := setupDeviceTest(t, true)
	ac := env.srv.agents.GetAgent(env.device.ID)
	require.NotNil(t, ac)
	// The stored value is the concrete conn; reach its field to simulate an
	// agent that never advertised the capability.
	ac.(*agentapi.AgentConn).Capabilities = nil

	w := doRequest(env.srv, http.MethodGet, "/api/v1/devices/"+env.device.ID.String()+"/logs", env.adminToken(t), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Zero(t, env.agentStream.Len())
}
