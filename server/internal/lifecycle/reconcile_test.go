package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

func TestReconcilerPurgesOrphanSeriesButKeepsLiveDevices(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	ctx := context.Background()

	tenant := uuid.New()
	live := seedDeviceWithTelemetry(t, f, tenant)
	orphan := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, f.vm.WriteSamples(ctx, tenant, orphan, []telemetry.Sample{
		{Name: "opengate_edge_metric_avg", Value: 9, TS: ts},
	}))
	require.NoError(t, f.vm.Flush(ctx))

	// The VictoriaMetrics instance is shared across tests, so the inventory is scoped to this tenant.
	inv := &tenantScopedInventory{inner: f.vm, tenant: tenant}
	rec := NewReconciler(inv, f.vm, NewPostgresPurger(f.store.DB(), nil), nil)
	purged, err := rec.Sweep(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, purged, 1, "the orphan must be swept")

	n, err := f.vm.CountSeries(ctx, tenant, &orphan)
	require.NoError(t, err)
	assert.Zero(t, n, "orphan series must be purged")

	n, err = f.vm.CountSeries(ctx, tenant, &live)
	require.NoError(t, err)
	assert.Positive(t, n, "a device with a Postgres row must not be swept")
	assert.Positive(t, countRows(t, f, dbtx.WithTenant(ctx, tenant, true), qDevices, live))

	purged, err = rec.Sweep(ctx)
	require.NoError(t, err)
	assert.Zero(t, purged, "reconcile is idempotent")
}

type tenantScopedInventory struct {
	inner  SubjectLister
	tenant uuid.UUID
}

func (o *tenantScopedInventory) ListSubjects(ctx context.Context) ([]telemetry.SeriesSubject, error) {
	all, err := o.inner.ListSubjects(ctx)
	if err != nil {
		return nil, err
	}
	var out []telemetry.SeriesSubject
	for _, s := range all {
		if s.TenantID == o.tenant {
			out = append(out, s)
		}
	}
	return out, nil
}
