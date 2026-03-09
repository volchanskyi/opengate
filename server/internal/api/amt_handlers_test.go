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

const (
	testAMTEmail   = "admin@test.com"
	testPathAMT    = "/api/v1/amt/devices"
	testPathAMTOne = "/api/v1/amt/devices/"
)

func TestListAMTDevicesEmpty(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)

	w := doRequest(srv, http.MethodGet, testPathAMT, token, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var devices []AMTDevice
	require.NoError(t, json.NewDecoder(w.Body).Decode(&devices))
	assert.Empty(t, devices)
}

func TestListAMTDevicesWithDevices(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)

	id := uuid.New()
	err := srv.store.UpsertAMTDevice(t.Context(), &db.AMTDevice{
		UUID:     id,
		Hostname: "amt-host",
		Model:    "ModelX",
		Firmware: "1.0.0",
		Status:   db.StatusOnline,
	})
	require.NoError(t, err)

	w := doRequest(srv, http.MethodGet, testPathAMT, token, nil)
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
	w := doRequest(srv, http.MethodGet, testPathAMT, "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetAMTDeviceFound(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)

	id := uuid.New()
	err := srv.store.UpsertAMTDevice(t.Context(), &db.AMTDevice{
		UUID:     id,
		Hostname: "found-host",
		Model:    "ModelY",
		Firmware: "2.0.0",
		Status:   db.StatusOnline,
	})
	require.NoError(t, err)

	w := doRequest(srv, http.MethodGet, testPathAMTOne+id.String(), token, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var device AMTDevice
	require.NoError(t, json.NewDecoder(w.Body).Decode(&device))
	assert.Equal(t, id, device.Uuid)
	assert.Equal(t, "found-host", device.Hostname)
}

func TestGetAMTDeviceNotFound(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)

	w := doRequest(srv, http.MethodGet, testPathAMTOne+uuid.New().String(), token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAmtPowerActionNotConnected(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr ApiError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, "device not connected", apiErr.Error)
}

func TestAmtPowerActionUnauthorized(t *testing.T) {
	srv, _ := newTestServer(t)
	body := AMTPowerRequest{Action: PowerOn}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", "", body)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
