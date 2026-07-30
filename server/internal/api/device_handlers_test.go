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
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const (
	testPathDevices  = "/api/v1/devices"
	testPathDevicesS = "/api/v1/devices/"
)

func TestDeviceHandlers(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	// Device reads are open to every member; moving and deleting a device are
	// configuration changes behind the admin gate.
	_, token := seedTestUser(t, srv, cfg, "dev@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "dev-admin@example.com", true)
	ctx := testTenantContext(t)

	group := &device.Group{ID: uuid.New(), Name: "test-group"}
	require.NoError(t, srv.groups.Create(ctx, group))

	dev := &device.Device{
		ID:       uuid.New(),
		GroupID:  group.ID,
		Hostname: "test-host",
		OS:       "linux",
		Status:   db.StatusOnline,
	}
	require.NoError(t, srv.devices.Upsert(ctx, dev))

	t.Run("list devices", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices+"?group_id="+group.ID.String(), token, nil)
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
		newGroup := &device.Group{ID: uuid.New(), Name: "new-group"}
		require.NoError(t, srv.groups.Create(ctx, newGroup))

		body := map[string]interface{}{"group_id": newGroup.ID.String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), adminToken, body)
		assert.Equal(t, http.StatusOK, w.Code)

		var d Device
		json.NewDecoder(w.Body).Decode(&d)
		assert.Equal(t, newGroup.ID, d.GroupId)
	})

	t.Run("update device group not found", func(t *testing.T) {
		body := map[string]interface{}{"group_id": uuid.New().String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), adminToken, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("update device group to the nil uuid ungroups the device", func(t *testing.T) {
		body := map[string]interface{}{"group_id": uuid.Nil.String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), adminToken, body)
		require.Equal(t, http.StatusOK, w.Code)

		var d Device
		require.NoError(t, json.NewDecoder(w.Body).Decode(&d))
		assert.Equal(t, uuid.Nil, d.GroupId)

		// The device stays reachable and re-groupable once ungrouped.
		regroup := map[string]interface{}{"group_id": group.ID.String()}
		w = doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), adminToken, regroup)
		require.Equal(t, http.StatusOK, w.Code)
		require.NoError(t, json.NewDecoder(w.Body).Decode(&d))
		assert.Equal(t, group.ID, d.GroupId)
	})

	t.Run("update device not found", func(t *testing.T) {
		body := map[string]interface{}{"group_id": uuid.New().String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+uuid.New().String(), adminToken, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete device", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+dev.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("list devices invalid group_id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices+"?group_id=not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get device invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+"not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("delete device invalid id", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+"not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("requires auth", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices+"?group_id="+group.ID.String(), "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// TestDeviceAMTProperty covers the shape of the amt object across the three
// states a device can be in. The badge reads it straight off this payload, so
// the device detail page issues no AMT request at all.
func TestDeviceAMTProperty(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "amt-device@example.com", false)
	ctx := testTenantContext(t)

	group := &device.Group{ID: uuid.New(), Name: "amt-group"}
	require.NoError(t, srv.groups.Create(ctx, group))

	seed := func(hostname string) *device.Device {
		d := &device.Device{ID: uuid.New(), GroupID: group.ID, Hostname: hostname, OS: "linux", Status: db.StatusOnline}
		require.NoError(t, srv.devices.Upsert(ctx, d))
		return d
	}
	available := true

	t.Run("absent when the device neither supports AMT nor has a link", func(t *testing.T) {
		d := seed("amt-absent")
		require.NoError(t, srv.hardware.Upsert(ctx, &device.Hardware{DeviceID: d.ID, CPUModel: "AMD Ryzen 9 7950X"}))

		amt := fetchDeviceAMTField(t, srv, token, d.ID)
		assert.Nil(t, amt, "a machine with no Management Engine carries no amt object")
	})

	t.Run("available but unlinked when the hardware supports AMT", func(t *testing.T) {
		d := seed("amt-capable")
		systemUUID := uuid.New()
		require.NoError(t, srv.hardware.Upsert(ctx, &device.Hardware{
			DeviceID:     d.ID,
			CPUModel:     "Intel Core i7-12700K",
			SystemUUID:   &systemUUID,
			AMTAvailable: &available,
			AMTVersion:   "16.1.30.2260",
		}))

		amt := fetchDeviceAMTField(t, srv, token, d.ID)
		require.NotNil(t, amt)
		assert.True(t, amt.Available, "the badge shows on capability alone, so it never flickers")
		assert.Nil(t, amt.Status)
		assert.Nil(t, amt.Uuid)
	})

	t.Run("carries status and uuid once a connection is linked", func(t *testing.T) {
		d := seed("amt-linked")
		systemUUID := uuid.New()
		require.NoError(t, srv.hardware.Upsert(ctx, &device.Hardware{
			DeviceID:     d.ID,
			CPUModel:     "Intel Core i5-1145G7",
			SystemUUID:   &systemUUID,
			AMTAvailable: &available,
		}))
		conn := testutil.SeedAMTDevice(t, ctx, srv.store, d.ID)

		amt := fetchDeviceAMTField(t, srv, token, d.ID)
		require.NotNil(t, amt)
		assert.True(t, amt.Available)
		require.NotNil(t, amt.Status)
		assert.Equal(t, DeviceAMTStatusOffline, *amt.Status)
		require.NotNil(t, amt.Uuid)
		assert.Equal(t, conn.UUID, *amt.Uuid)
	})
}

// TestDeviceResponseNeverLeaksSystemUUID guards the locked decision that the
// join key is stored but never returned.
func TestDeviceResponseNeverLeaksSystemUUID(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "amt-leak@example.com", false)
	ctx := testTenantContext(t)

	group := &device.Group{ID: uuid.New(), Name: "leak-group"}
	require.NoError(t, srv.groups.Create(ctx, group))
	d := &device.Device{ID: uuid.New(), GroupID: group.ID, Hostname: "leak-host", OS: "linux", Status: db.StatusOnline}
	require.NoError(t, srv.devices.Upsert(ctx, d))

	systemUUID := uuid.New()
	available := true
	require.NoError(t, srv.hardware.Upsert(ctx, &device.Hardware{
		DeviceID:     d.ID,
		CPUModel:     "Intel Core i7-12700K",
		SystemUUID:   &systemUUID,
		AMTAvailable: &available,
	}))

	for _, path := range []string{testPathDevicesS + d.ID.String(), testPathDevicesS + d.ID.String() + "/hardware"} {
		w := doRequest(srv, http.MethodGet, path, token, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		body := w.Body.String()
		assert.NotContains(t, body, systemUUID.String(), "%s must not return the system UUID", path)
		assert.NotContains(t, body, "system_uuid", "%s must not name the join key", path)
	}
}

// fetchDeviceAMTField reads one device and returns its decoded amt object.
func fetchDeviceAMTField(t *testing.T, srv *Server, token string, deviceID uuid.UUID) *DeviceAMT {
	t.Helper()
	w := doRequest(srv, http.MethodGet, testPathDevicesS+deviceID.String(), token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var got Device
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	return got.Amt
}
