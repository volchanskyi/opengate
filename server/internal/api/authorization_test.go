package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

// TestDeviceIDOR verifies that users cannot access devices belonging to other users' groups.
func TestDeviceIDOR(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	owner, ownerToken := seedTestUser(t, srv, cfg, "owner@example.com", false)
	_, attackerToken := seedTestUser(t, srv, cfg, "attacker@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-idor@example.com", true)
	// Add admin to Administrators security group.
	admin, _ := srv.users.GetByEmail(ctx, "admin-idor@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store, owner.ID)
	device := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	t.Run("get device owner succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+device.ID.String(), ownerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get device attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+device.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("get device admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+device.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("list devices with group_id owner succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices+"?group_id="+group.ID.String(), ownerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var devices []Device
		json.NewDecoder(w.Body).Decode(&devices)
		assert.Len(t, devices, 1)
	})

	t.Run("list devices with group_id attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices+"?group_id="+group.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("list devices without group_id non-admin returns own devices only", func(t *testing.T) {
		// Attacker has no groups → should get empty list.
		w := doRequest(srv, http.MethodGet, testPathDevices, attackerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var devices []Device
		json.NewDecoder(w.Body).Decode(&devices)
		assert.Empty(t, devices)
	})

	t.Run("delete device attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+device.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete device admin succeeds", func(t *testing.T) {
		// Create a disposable device for this subtest.
		d2 := testutil.SeedDevice(t, ctx, srv.store, group.ID)
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+d2.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
