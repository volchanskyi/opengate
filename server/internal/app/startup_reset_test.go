package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAStartMarksEveryTenantsMachinesOffline(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(t)
	devices := testutil.NewTestDevices(t, cfg.Store)
	var machines []struct {
		ctx context.Context
		id  uuid.UUID
	}
	for _, name := range []string{"Contoso", "Northwind"} {
		tenant := uuid.New()
		testutil.EnsureTenant(t, context.Background(), cfg.Store, tenant, name+"-"+tenant.String()[:8])
		ctx := dbtx.WithTenant(context.Background(), tenant, false)
		machine := testutil.SeedDeviceIn(t, ctx, cfg.Store, testutil.SeedOrganization(t, ctx, cfg.Store, name), uuid.Nil)
		require.NoError(t, devices.SetStatus(ctx, machine.ID, device.StatusOnline))
		machines = append(machines, struct {
			ctx context.Context
			id  uuid.UUID
		}{ctx, machine.ID})
	}

	_, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)

	for _, m := range machines {
		stored, err := devices.Get(m.ctx, m.id)
		require.NoError(t, err)
		assert.Equal(t, device.StatusOffline, stored.Status, "a status left online by the last run is cleared")
	}
}
