package api

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

// TestDeviceIDOR verifies that users cannot access devices belonging to other users' groups.
func TestDeviceIDOR(t *testing.T) {
	srv, cfg := newTestServer(t)
	ctx := t.Context()

	owner, ownerToken := seedTestUser(t, srv, cfg, "owner@example.com", false)
	_, attackerToken := seedTestUser(t, srv, cfg, "attacker@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-idor@example.com", true)
	// Add admin to Administrators security group.
	admin, _ := srv.store.GetUserByEmail(ctx, "admin-idor@example.com")
	require.NoError(t, srv.store.AddSecurityGroupMember(ctx, db.AdminGroupID, admin.ID))

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

// TestGroupIDOR verifies that users cannot access other users' groups.
func TestGroupIDOR(t *testing.T) {
	srv, cfg := newTestServer(t)
	ctx := t.Context()

	owner, ownerToken := seedTestUser(t, srv, cfg, "group-owner@example.com", false)
	_, attackerToken := seedTestUser(t, srv, cfg, "group-attacker@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "group-admin@example.com", true)
	admin, _ := srv.store.GetUserByEmail(ctx, "group-admin@example.com")
	require.NoError(t, srv.store.AddSecurityGroupMember(ctx, db.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store, owner.ID)

	t.Run("get group owner succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), ownerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get group attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("get group admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroupsS+group.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("delete group attacker forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+group.ID.String(), attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete group admin succeeds", func(t *testing.T) {
		g2 := testutil.SeedGroup(t, ctx, srv.store, owner.ID)
		w := doRequest(srv, http.MethodDelete, testPathGroupsS+g2.ID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("list groups returns only own groups", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, testPathGroups, attackerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var groups []Group
		json.NewDecoder(w.Body).Decode(&groups)
		assert.Empty(t, groups)
	})
}

// TestSessionIDOR verifies session ownership checks.
func TestSessionIDOR(t *testing.T) {
	srv, cfg := newTestServer(t)
	ctx := t.Context()

	owner, ownerToken := seedTestUser(t, srv, cfg, "sess-owner@example.com", false)
	_, attackerToken := seedTestUser(t, srv, cfg, "sess-attacker@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "sess-admin@example.com", true)
	admin, _ := srv.store.GetUserByEmail(ctx, "sess-admin@example.com")
	require.NoError(t, srv.store.AddSecurityGroupMember(ctx, db.AdminGroupID, admin.ID))

	group := testutil.SeedGroup(t, ctx, srv.store, owner.ID)
	device := testutil.SeedDevice(t, ctx, srv.store, group.ID)
	sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, owner.ID)

	t.Run("delete session owner succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess.Token, ownerToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("delete session attacker forbidden", func(t *testing.T) {
		sess2 := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, owner.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess2.Token, attackerToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete session admin succeeds", func(t *testing.T) {
		sess3 := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, owner.ID)
		w := doRequest(srv, http.MethodDelete, "/api/v1/sessions/"+sess3.Token, adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// TestAMTAdminOnly verifies that all AMT endpoints require admin access.
func TestAMTAdminOnly(t *testing.T) {
	srv, cfg := newTestServer(t)
	ctx := t.Context()

	_, regularToken := seedTestUser(t, srv, cfg, "amt-regular@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "amt-admin@example.com", true)
	admin, _ := srv.store.GetUserByEmail(ctx, "amt-admin@example.com")
	require.NoError(t, srv.store.AddSecurityGroupMember(ctx, db.AdminGroupID, admin.ID))

	amtDevice := testutil.SeedAMTDevice(t, ctx, srv.store)

	t.Run("list AMT devices regular forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices", regularToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("list AMT devices admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("get AMT device regular forbidden", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices/"+amtDevice.UUID.String(), regularToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("get AMT device admin succeeds", func(t *testing.T) {
		w := doRequest(srv, http.MethodGet, "/api/v1/amt/devices/"+amtDevice.UUID.String(), adminToken, nil)
		// Could be 200 or 404 depending on whether AMT device is stored — just not 403.
		assert.NotEqual(t, http.StatusForbidden, w.Code)
	})

	t.Run("AMT power action regular forbidden", func(t *testing.T) {
		body := map[string]string{"action": "PowerOn"}
		w := doRequest(srv, http.MethodPost, "/api/v1/amt/devices/"+amtDevice.UUID.String()+"/power", regularToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// TestPasswordValidation verifies password length constraints during registration.
func TestPasswordValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name     string
		password string
		status   int
	}{
		{"too short", "1234567", http.StatusBadRequest},
		{"minimum length", "12345678", http.StatusCreated},
		{"normal length", "password123", http.StatusCreated},
		{"at bcrypt limit 72 chars", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890123456789", http.StatusCreated},
		{"over bcrypt limit 73 chars", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ012345678901234567890", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := uuid.New().String()[:8] + "@example.com"
			body := map[string]string{"email": email, "password": tt.password}
			w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
			assert.Equal(t, tt.status, w.Code, "password length: %d", len(tt.password))
		})
	}
}
