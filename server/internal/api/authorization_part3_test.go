package api

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

// TestAMTAdminOnly verifies that all AMT endpoints require admin access.
func TestAMTAdminOnly(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	_, regularToken := seedTestUser(t, srv, cfg, "amt-regular@example.com", false)
	_, adminToken := seedTestUser(t, srv, cfg, "amt-admin@example.com", true)
	admin, _ := srv.users.GetByEmail(ctx, "amt-admin@example.com")
	require.NoError(t, srv.securityGroups.AddMember(ctx, auth.AdminGroupID, admin.ID))

	owner := testutil.SeedUser(t, ctx, srv.store)
	group := testutil.SeedGroup(t, ctx, srv.store, owner.ID)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)
	amtDevice := testutil.SeedAMTDevice(t, ctx, srv.store, dev.ID)

	t.Run("AMT power action regular forbidden", func(t *testing.T) {
		body := map[string]string{"action": "power_on"}
		w := doRequest(srv, http.MethodPost, "/api/v1/amt/devices/"+amtDevice.UUID.String()+"/power", regularToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("AMT power action admin reaches the operator", func(t *testing.T) {
		body := map[string]string{"action": "power_on"}
		w := doRequest(srv, http.MethodPost, "/api/v1/amt/devices/"+amtDevice.UUID.String()+"/power", adminToken, body)
		// The device has no live CIRA tunnel, so the operator refuses with 409 —
		// which is the proof the request cleared the admin gate.
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}

// TestPasswordValidation verifies password length constraints during registration.
func TestPasswordValidation(t *testing.T) {
	t.Parallel()
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
