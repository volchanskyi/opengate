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
	testPathDevices  = "/api/v1/devices"
	testPathDevicesS = "/api/v1/devices/"
)

func TestDeviceHandlers(t *testing.T) {
	srv, cfg := newTestServer(t)
	user, token := seedTestUser(t, srv, cfg, "dev@example.com", false)

	group := &db.Group{ID: uuid.New(), Name: "test-group", OwnerID: user.ID}
	require.NoError(t, srv.store.CreateGroup(t.Context(), group))

	device := &db.Device{
		ID:       uuid.New(),
		GroupID:  group.ID,
		Hostname: "test-host",
		OS:       "linux",
		Status:   db.StatusOnline,
	}
	require.NoError(t, srv.store.UpsertDevice(t.Context(), device))

	t.Run("list devices", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices + "?group_id="+group.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var devices []*db.Device
		json.NewDecoder(w.Body).Decode(&devices)
		assert.Len(t, devices, 1)
		assert.Equal(t, device.ID, devices[0].ID)
	})

	t.Run("list devices missing group_id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices, token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get device", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+device.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var d db.Device
		json.NewDecoder(w.Body).Decode(&d)
		assert.Equal(t, device.Hostname, d.Hostname)
	})

	t.Run("get device not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete device", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+device.ID.String(), token, nil)
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
