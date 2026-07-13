package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
)

// fakeInventoryRepo records the device it was queried for and returns a canned
// footprint, so the handler test does not need a live discovery ingest.
type fakeInventoryRepo struct {
	gotDevice  uuid.UUID
	components []inventory.Component
	err        error
	called     int
}

func (f *fakeInventoryRepo) Replace(context.Context, uuid.UUID, time.Time, []inventory.Component) error {
	return nil
}

func (f *fakeInventoryRepo) ListForDevice(_ context.Context, deviceID uuid.UUID, _ int) ([]inventory.Component, error) {
	f.gotDevice = deviceID
	f.called++
	return f.components, f.err
}

func TestGetDeviceInventoryHandler(t *testing.T) {
	srv, cfg := newTestServer(t)
	user, token := seedTestUser(t, srv, cfg, "inv@example.com", false)
	ctx := testTenantContext(t)

	group := &device.Group{ID: uuid.New(), Name: "inv-group", OwnerID: user.ID}
	require.NoError(t, srv.groups.Create(ctx, group))
	dev := &device.Device{ID: uuid.New(), GroupID: group.ID, Hostname: "inv-host", OS: "linux", Status: db.StatusOnline}
	require.NoError(t, srv.devices.Upsert(ctx, dev))

	path := "/api/v1/devices/" + dev.ID.String() + "/inventory"

	t.Run("503 when inventory not configured", func(t *testing.T) {
		srv.inventory = nil
		w := doRequest(srv, http.MethodGet, path, token, nil)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("200 maps the device footprint scoped to the path device", func(t *testing.T) {
		seen := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
		fake := &fakeInventoryRepo{components: []inventory.Component{
			{Kind: inventory.KindPort, Name: "postgres", Proto: "tcp", Port: 5432, FirstSeen: seen, LastSeen: seen},
			{Kind: inventory.KindPackage, Name: "openssl", Version: "3.0.13", FirstSeen: seen, LastSeen: seen},
		}}
		srv.inventory = fake

		w := doRequest(srv, http.MethodGet, path, token, nil)
		require.Equal(t, http.StatusOK, w.Code)

		var resp DeviceInventory
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, dev.ID, resp.DeviceId)
		require.Len(t, resp.Items, 2)
		assert.Equal(t, InventoryItemKind("port"), resp.Items[0].Kind)
		assert.Equal(t, "postgres", resp.Items[0].Name)
		assert.Equal(t, 5432, resp.Items[0].Port)
		assert.Equal(t, "tcp", resp.Items[0].Proto)
		assert.Equal(t, "openssl", resp.Items[1].Name)
		assert.Equal(t, "3.0.13", resp.Items[1].Version)

		assert.Equal(t, 1, fake.called)
		assert.Equal(t, dev.ID, fake.gotDevice)
	})

	t.Run("404 for an unknown device does not reach the repo", func(t *testing.T) {
		fake := &fakeInventoryRepo{}
		srv.inventory = fake
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+uuid.New().String()+"/inventory", token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, 0, fake.called)
	})

	t.Run("403 when the caller does not own the device group", func(t *testing.T) {
		other, _ := seedTestUser(t, srv, cfg, "inv-other@example.com", false)
		otherGroup := &device.Group{ID: uuid.New(), Name: "inv-other-group", OwnerID: other.ID}
		require.NoError(t, srv.groups.Create(ctx, otherGroup))
		otherDev := &device.Device{ID: uuid.New(), GroupID: otherGroup.ID, Hostname: "other-host", OS: "linux", Status: db.StatusOnline}
		require.NoError(t, srv.devices.Upsert(ctx, otherDev))

		fake := &fakeInventoryRepo{}
		srv.inventory = fake
		w := doRequest(srv, http.MethodGet, "/api/v1/devices/"+otherDev.ID.String()+"/inventory", token, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, 0, fake.called, "ownership is checked before the repo is touched")
	})
}
