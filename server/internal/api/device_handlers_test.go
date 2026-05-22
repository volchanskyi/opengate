package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
)

const (
	testPathDevices  = "/api/v1/devices"
	testPathDevicesS = "/api/v1/devices/"
)

func TestDeviceHandlers(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, token := seedTestUser(t, srv, cfg, "dev@example.com", false)

	group := &device.Group{ID: uuid.New(), Name: "test-group", OwnerID: user.ID}
	require.NoError(t, srv.groups.Create(t.Context(), group))

	dev := &device.Device{
		ID:       uuid.New(),
		GroupID:  group.ID,
		Hostname: "test-host",
		OS:       "linux",
		Status:   db.StatusOnline,
	}
	require.NoError(t, srv.devices.Upsert(t.Context(), dev))

	t.Run("list devices", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices + "?group_id="+group.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var devices []*device.Device
		json.NewDecoder(w.Body).Decode(&devices)
		assert.Len(t, devices, 1)
		assert.Equal(t, dev.ID, devices[0].ID)
	})

	t.Run("list all devices without group_id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices, token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var devices []*device.Device
		json.NewDecoder(w.Body).Decode(&devices)
		assert.GreaterOrEqual(t, len(devices), 1)
	})

	t.Run("get device", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+dev.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var d device.Device
		json.NewDecoder(w.Body).Decode(&d)
		assert.Equal(t, dev.Hostname, d.Hostname)
	})

	t.Run("get device not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("update device group", func(t *testing.T) {
		newGroup := &device.Group{ID: uuid.New(), Name: "new-group", OwnerID: user.ID}
		require.NoError(t, srv.groups.Create(t.Context(), newGroup))

		body := map[string]interface{}{"group_id": newGroup.ID.String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), token, body)
		assert.Equal(t, http.StatusOK, w.Code)

		var d Device
		json.NewDecoder(w.Body).Decode(&d)
		assert.Equal(t, newGroup.ID, d.GroupId)
	})

	t.Run("update device group not found", func(t *testing.T) {
		body := map[string]interface{}{"group_id": uuid.New().String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), token, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("update device not found", func(t *testing.T) {
		body := map[string]interface{}{"group_id": uuid.New().String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+uuid.New().String(), token, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete device", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+dev.ID.String(), token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("list devices invalid group_id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices + "?group_id=not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get device invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS + "not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete device invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS + "not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("requires auth", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices + "?group_id="+group.ID.String(), "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
