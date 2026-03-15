package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAMTListDevicesEmpty(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodGet, "/api/v1/amt/devices", adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var devices []json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&devices))
	assert.Empty(t, devices)
}

func TestAMTListDevicesWithSeeded(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Seed AMT devices
	d1 := testutil.SeedAMTDevice(t, ctx, env.store)
	d2 := testutil.SeedAMTDevice(t, ctx, env.store)

	resp := env.doJSON(t, http.MethodGet, "/api/v1/amt/devices", adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var devices []struct {
		UUID     uuid.UUID `json:"uuid"`
		Hostname string    `json:"hostname"`
		Status   string    `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&devices))
	assert.Len(t, devices, 2)

	// Check both devices are present
	uuids := map[uuid.UUID]bool{}
	for _, d := range devices {
		uuids[d.UUID] = true
	}
	assert.True(t, uuids[d1.UUID], "first device should be in list")
	assert.True(t, uuids[d2.UUID], "second device should be in list")
}

func TestAMTGetDeviceNotFound(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	resp := env.doJSON(t, http.MethodGet, "/api/v1/amt/devices/"+uuid.New().String(), adminToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAMTPowerActionDeviceNotConnected(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Seed an AMT device (not connected — no MPS)
	amtDevice := testutil.SeedAMTDevice(t, ctx, env.store)

	// Try to power action on disconnected device
	resp := env.doJSON(t, http.MethodPost, "/api/v1/amt/devices/"+amtDevice.UUID.String()+"/power", adminToken, map[string]string{
		"action": "power_on",
	})
	defer resp.Body.Close()

	// Should return 409 — device not connected
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var errResp struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
	assert.Contains(t, errResp.Error, "not connected")

	// Verify device is still in DB unchanged
	d, err := env.store.GetAMTDevice(ctx, amtDevice.UUID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusOffline, d.Status)
}
