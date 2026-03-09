package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func TestListAMTDevicesEmpty(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "admin@test.com", true)

	w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var devices []AMTDevice
	require.NoError(t, json.NewDecoder(w.Body).Decode(&devices))
	assert.Empty(t, devices)
}

func TestListAMTDevicesWithDevices(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "admin@test.com", true)

	id := uuid.New()
	err := srv.store.UpsertAMTDevice(t.Context(), &db.AMTDevice{
		UUID:     id,
		Hostname: "amt-host",
		Model:    "ModelX",
		Firmware: "1.0.0",
		Status:   db.StatusOnline,
	})
	require.NoError(t, err)

	w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var devices []AMTDevice
	require.NoError(t, json.NewDecoder(w.Body).Decode(&devices))
	require.Len(t, devices, 1)
	assert.Equal(t, id, devices[0].Uuid)
	assert.Equal(t, "amt-host", devices[0].Hostname)
	assert.Equal(t, AMTDeviceStatusOnline, devices[0].Status)
}

func TestListAMTDevicesUnauthorized(t *testing.T) {
	srv, _ := newTestServer(t)
	w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetAMTDeviceFound(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "admin@test.com", true)

	id := uuid.New()
	err := srv.store.UpsertAMTDevice(t.Context(), &db.AMTDevice{
		UUID:     id,
		Hostname: "found-host",
		Model:    "ModelY",
		Firmware: "2.0.0",
		Status:   db.StatusOnline,
	})
	require.NoError(t, err)

	w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices/"+id.String(), token, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var device AMTDevice
	require.NoError(t, json.NewDecoder(w.Body).Decode(&device))
	assert.Equal(t, id, device.Uuid)
	assert.Equal(t, "found-host", device.Hostname)
}

func TestGetAMTDeviceNotFound(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "admin@test.com", true)

	w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices/"+uuid.New().String(), token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAmtPowerActionNotConnected(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "admin@test.com", true)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, "/api/v1/amt/devices/"+uuid.New().String()+"/power", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr ApiError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Contains(t, apiErr.Error, "not connected")
}

func TestAmtPowerActionUnauthorized(t *testing.T) {
	srv, _ := newTestServer(t)
	body := AMTPowerRequest{Action: PowerOn}
	w := doRequest(srv, http.MethodPost, "/api/v1/amt/devices/"+uuid.New().String()+"/power", "", body)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
