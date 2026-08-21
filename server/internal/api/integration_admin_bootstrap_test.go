// The record a permission change leaves behind, and how the first operator on a
// fresh installation comes to hold administration at all.
package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestSecurityGroup_AuditLogging(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := t.Context()

	adminUser, adminPass := testutil.SeedAdminUser(t, ctx, env.store)
	adminToken := env.login(t, adminUser.Email, adminPass)

	// Add a user to trigger audit event.
	regularToken := env.register(t, "audit-sg@example.com", "pass1234")
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, regularToken, nil)
	var regUser db.User
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&regUser))
	resp.Body.Close()

	resp = env.doJSON(t, http.MethodPost, pathSecurityGroups+"/"+auth.AdminGroupID.String()+"/members",
		adminToken, map[string]string{"user_id": regUser.ID.String()})
	resp.Body.Close()

	// Audit writes are async — poll until the add_member event surfaces.
	var events []audit.Event
	require.Eventually(t, func() bool {
		r := env.doJSON(t, http.MethodGet, "/api/v1/audit?action=security_group.add_member", adminToken, nil)
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			return false
		}
		events = nil
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			return false
		}
		return len(events) > 0
	}, 3*time.Second, 50*time.Millisecond, "audit log should contain security_group.add_member event")
	assert.NotEmpty(t, events)
}

func TestAdminFirstUserBootstrap(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// First user registered becomes admin.
	firstToken := env.register(t, "first@example.com", "pass1234")

	resp := env.doJSON(t, http.MethodGet, "/api/v1/users", firstToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "first registered user should be admin")
}

func TestAdminSecondUserNotAdmin(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// First user becomes admin.
	_ = env.register(t, "bootstrap-first@example.com", "pass1234")

	// Second user should NOT be admin.
	secondToken := env.register(t, "bootstrap-second@example.com", "pass1234")

	resp := env.doJSON(t, http.MethodGet, "/api/v1/users", secondToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "second registered user should not be admin")
}
