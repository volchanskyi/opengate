package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestDeviceFleetReadsAreOrgWide verifies that organization membership alone
// grants the fleet read surface: two ordinary members of the same organization
// see the same devices, whichever group holds them.
func TestDeviceFleetReadsAreOrgWide(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	_, memberAToken := seedTestUser(t, srv, cfg, "member-a@example.com", false)
	_, memberBToken := seedTestUser(t, srv, cfg, "member-b@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-fleet@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "admin-fleet@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	for name, token := range map[string]string{
		"member a": memberAToken,
		"member b": memberBToken,
		"admin":    adminToken,
	} {
		t.Run("get device "+name, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, testPathDevicesS+dev.ID.String(), token, nil)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("list devices "+name, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, testPathDevices, token, nil)
			require.Equal(t, http.StatusOK, w.Code)
			var devices []Device
			require.NoError(t, json.NewDecoder(w.Body).Decode(&devices))
			assert.Len(t, devices, 1)
		})

		t.Run("list devices by group "+name, func(t *testing.T) {
			w := doRequest(srv, http.MethodGet, testPathDevices+"?group_id="+group.ID.String(), token, nil)
			require.Equal(t, http.StatusOK, w.Code)
			var devices []Device
			require.NoError(t, json.NewDecoder(w.Body).Decode(&devices))
			assert.Len(t, devices, 1)
		})
	}
}

// TestDeviceConfigurationIsAdminOnly verifies the mutation boundary: deleting a
// device and moving it between groups are configuration changes, refused to an
// ordinary member and allowed to an admin.
func TestDeviceConfigurationIsAdminOnly(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	_, memberToken := seedTestUser(t, srv, cfg, "cfg-member@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "cfg-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "cfg-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store)
	target := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	t.Run("move device member forbidden", func(t *testing.T) {
		body := map[string]string{"group_id": target.ID.String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), memberToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("move device admin succeeds", func(t *testing.T) {
		body := map[string]string{"group_id": target.ID.String()}
		w := doRequest(srv, http.MethodPatch, testPathDevicesS+dev.ID.String(), adminToken, body)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("delete device member forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+dev.ID.String(), memberToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete device admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathDevicesS+dev.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// TestDeviceCommandsAreOpenToOrgMembers verifies that acting on a device —
// restarting the agent, toggling maintenance — needs organization membership
// only. A 409 proves the request cleared authorization and reached the agent
// broker, which has no connected agent in this server.
func TestDeviceCommandsAreOpenToOrgMembers(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	_, memberToken := seedTestUser(t, srv, cfg, "cmd-member@example.com", false)

	group := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	t.Run("restart reaches the agent broker", func(t *testing.T) {
		w := doRequest(srv, http.MethodPost, testPathDevicesS+dev.ID.String()+"/restart", memberToken, map[string]string{})
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("maintenance toggle succeeds", func(t *testing.T) {
		body := map[string]any{"enabled": true, "reason": "patching"}
		w := doRequest(srv, http.MethodPost, testPathDevicesS+dev.ID.String()+"/maintenance", memberToken, body)
		require.Equal(t, http.StatusOK, w.Code)
		var updated Device
		require.NoError(t, json.NewDecoder(w.Body).Decode(&updated))
		require.NotNil(t, updated.MaintenanceOn)
		assert.True(t, *updated.MaintenanceOn)
	})

	t.Run("create session reaches the agent broker", func(t *testing.T) {
		body := map[string]any{"device_id": dev.ID.String()}
		w := doRequest(srv, http.MethodPost, "/api/v1/sessions", memberToken, body)
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}

// TestDeviceReadsRejectOtherOrganizations verifies the visibility boundary still
// stops at the organization: a member of another organization gets a 404 from
// the tenant-scoped lookup rather than the device.
func TestDeviceReadsRejectOtherOrganizations(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	group := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	outsider, _ := seedTestUser(t, srv, cfg, "outsider@example.com", false)
	outsiderToken, err := cfg.GenerateToken(outsider.ID, outsider.Email, false, uuid.New())
	require.NoError(t, err)

	t.Run("get device other org not found", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevicesS+dev.ID.String(), outsiderToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("list devices other org is empty", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathDevices, outsiderToken, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var devices []Device
		require.NoError(t, json.NewDecoder(w.Body).Decode(&devices))
		assert.Empty(t, devices)
	})
}
