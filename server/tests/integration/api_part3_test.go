package integration

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"net/http"
	"testing"
)

func TestDeviceLifecycle(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	token := env.register(t, "devops@example.com", "pass1234")

	// Get current user to know the owner ID
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, token, nil)
	var user db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	resp.Body.Close()

	// Create a group via API
	resp = env.doJSON(t, http.MethodPost, pathGroups, token, map[string]string{"name": "prod-servers"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var group device.Group
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&group))
	resp.Body.Close()
	assert.Equal(t, "prod-servers", group.Name)

	// Seed a device directly into the store (agents register via agentapi, not REST)
	d := &device.Device{
		ID:       uuid.New(),
		GroupID:  group.ID,
		Hostname: webServer01,
		OS:       "linux",
		Status:   db.StatusOnline,
	}
	require.NoError(t, env.devices.Upsert(defaultTenantContext(), d))

	t.Run("list devices in group", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/devices?group_id="+group.ID.String(), token, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var devices []*device.Device
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&devices))
		require.Len(t, devices, 1)
		assert.Equal(t, webServer01, devices[0].Hostname)
	})

	t.Run("get single device", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, "/api/v1/devices/"+d.ID.String(), token, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var d device.Device
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&d))
		assert.Equal(t, webServer01, d.Hostname)
		assert.Equal(t, "linux", d.OS)
	})

	t.Run("delete group ungroups devices", func(t *testing.T) {
		// Delete the group
		resp := env.doJSON(t, http.MethodDelete, "/api/v1/groups/"+group.ID.String(), token, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Device should still exist but with null group_id
		resp2 := env.doJSON(t, http.MethodGet, "/api/v1/devices/"+d.ID.String(), token, nil)
		defer resp2.Body.Close()
		assert.Equal(t, http.StatusOK, resp2.StatusCode)
	})
}
