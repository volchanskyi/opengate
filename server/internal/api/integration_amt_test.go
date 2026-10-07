package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestDeviceCarriesItsAMTProperty(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	site := testutil.SeedSite(t, ctx, env.store)
	hardware := testutil.NewTestHardware(t, env.store)
	tenantCtx := defaultTenantContext()

	plain := testutil.SeedDevice(t, ctx, env.store, site.ID)
	require.NoError(t, hardware.Upsert(tenantCtx, &device.Hardware{DeviceID: plain.ID, CPUModel: "AMD Ryzen 9 7950X"}))

	capable := testutil.SeedDevice(t, ctx, env.store, site.ID)
	available := true
	require.NoError(t, hardware.Upsert(tenantCtx, &device.Hardware{
		DeviceID:     capable.ID,
		CPUModel:     "Intel Core i7-12700K",
		SystemUUID:   ptr(uuid.New()),
		AMTAvailable: &available,
		AMTVersion:   "16.1.30.2260",
	}))

	linked := testutil.SeedDevice(t, ctx, env.store, site.ID)
	require.NoError(t, hardware.Upsert(tenantCtx, &device.Hardware{
		DeviceID:     linked.ID,
		CPUModel:     "Intel Core i5-1145G7",
		SystemUUID:   ptr(uuid.New()),
		AMTAvailable: &available,
		AMTVersion:   "16.1.30.2260",
	}))
	amtConn := testutil.SeedAMTDevice(t, ctx, env.store, linked.ID)

	t.Run("device without AMT carries no amt object", func(t *testing.T) {
		assert.Nil(t, fetchDeviceAMT(t, env, adminToken, plain.ID))
	})

	t.Run("AMT-capable device that never dialled in", func(t *testing.T) {
		amt := fetchDeviceAMT(t, env, adminToken, capable.ID)
		require.NotNil(t, amt, "the badge must show on capability alone")
		assert.True(t, amt.Available)
		assert.Empty(t, amt.Status, "no connection has ever been linked")
		assert.Nil(t, amt.UUID)
	})

	t.Run("AMT-capable device with a linked connection", func(t *testing.T) {
		amt := fetchDeviceAMT(t, env, adminToken, linked.ID)
		require.NotNil(t, amt)
		assert.True(t, amt.Available)
		assert.Equal(t, "offline", amt.Status)
		require.NotNil(t, amt.UUID)
		assert.Equal(t, amtConn.UUID, *amt.UUID)
	})
}

func TestAMTPowerActionDeviceNotConnected(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	site := testutil.SeedSite(t, ctx, env.store)
	dev := testutil.SeedDevice(t, ctx, env.store, site.ID)
	amtDevice := testutil.SeedAMTDevice(t, ctx, env.store, dev.ID)

	resp := env.doJSON(t, http.MethodPost, "/api/v1/amt/devices/"+amtDevice.UUID.String()+"/power", adminToken, map[string]string{
		"action": "power_on",
	})
	defer resp.Body.Close()

	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var errResp struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
	assert.Contains(t, errResp.Error, "not connected")

	amt := fetchDeviceAMT(t, env, adminToken, dev.ID)
	require.NotNil(t, amt)
	assert.Equal(t, "offline", amt.Status)
}

type deviceAMT struct {
	Available bool       `json:"available"`
	Status    string     `json:"status"`
	UUID      *uuid.UUID `json:"uuid"`
}

func fetchDeviceAMT(t *testing.T, env *testEnv, token string, deviceID uuid.UUID) *deviceAMT {
	t.Helper()
	resp := env.doJSON(t, http.MethodGet, "/api/v1/devices/"+deviceID.String(), token, nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		AMT *deviceAMT `json:"amt"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	return payload.AMT
}

func ptr[T any](v T) *T { return &v }
