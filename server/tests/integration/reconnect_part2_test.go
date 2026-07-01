package integration

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
	"time"
)

func TestAgentReconnectNewCert(t *testing.T) {
	t.Parallel()
	env := newAgentTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	// Connect agent
	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Disconnect
	stream.Close()

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOffline
	}, 5*time.Second, 100*time.Millisecond)

	// Reconnect — connectAgentWithID signs a fresh cert each time,
	// simulating a cert rotation scenario. The device ID stays the same.
	_ = env.connectAgentWithID(t, deviceID)

	// Device should come back online with the new cert
	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)
}
