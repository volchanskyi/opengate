package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
	"time"
)

// TestUpdatePushSkipsCurrentVersion verifies that agents already on the
// target version are not pushed an update.
func TestUpdatePushSkipsCurrentVersion(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, admin.ID)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	// Connect agent — it will register with the version from AGENT_VERSION env
	// (defaults to Cargo.toml version). We publish a manifest matching that version.
	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Get the agent's reported version from the DB
	d, err := env.devices.Get(defaultTenantContext(), deviceID)
	require.NoError(t, err)
	agentVersion := d.AgentVersion

	// Publish manifest with the same version the agent already reports
	publishManifest(t, env, adminJWT, agentVersion, "linux", "amd64")

	// Push should skip — agent is already on this version
	result := pushUpdate(t, env, adminJWT, agentVersion, "linux", "amd64")
	assert.Equal(t, 0, result.PushedCount, "agent already on target version should be skipped")
}

// TestUpdatePushNoMatchingOS verifies that agents with non-matching OS/arch
// are not pushed an update.
func TestUpdatePushNoMatchingOS(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, admin.ID)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	// Connect agent — registers as linux/amd64
	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Publish manifest for windows/amd64 — won't match the linux agent
	publishManifest(t, env, adminJWT, "0.15.0", "windows", "amd64")

	// Push for windows/amd64 — linux agent should not be targeted
	result := pushUpdate(t, env, adminJWT, "0.15.0", "windows", "amd64")
	assert.Equal(t, 0, result.PushedCount, "linux agent should not get windows update")
}
