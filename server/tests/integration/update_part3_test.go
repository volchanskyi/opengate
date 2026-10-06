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

func TestUpdatePushSkipsCurrentVersion(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	site := testutil.SeedSite(t, ctx, env.store)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	_, deviceID := env.connectAgent(t, site.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	d, err := env.devices.Get(defaultTenantContext(), deviceID)
	require.NoError(t, err)
	agentVersion := d.AgentVersion

	publishManifest(t, env, adminJWT, agentVersion, "linux", "amd64")

	result := pushUpdate(t, env, adminJWT, agentVersion, "linux", "amd64")
	assert.Equal(t, 0, result.PushedCount, "agent already on target version should be skipped")
}

func TestUpdatePushNoMatchingOS(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	admin, _ := testutil.SeedAdminUser(t, ctx, env.store)
	site := testutil.SeedSite(t, ctx, env.store)

	adminJWT, err := env.jwt.GenerateToken(admin.ID, admin.Email, admin.IsAdmin)
	require.NoError(t, err)

	_, deviceID := env.connectAgent(t, site.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	publishManifest(t, env, adminJWT, "0.15.0", "windows", "amd64")

	result := pushUpdate(t, env, adminJWT, "0.15.0", "windows", "amd64")
	assert.Equal(t, 0, result.PushedCount, "linux agent should not get windows update")
}
