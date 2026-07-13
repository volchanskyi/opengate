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

	org := uuid.New()
	// A live device with a real Postgres row and series.
	live := seedDeviceWithTelemetry(t, f, org)
	// An orphan: series in VM but no Postgres device row (e.g. left by a purge
	// that failed before Postgres delete and never resumed).
	orphan := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, f.vm.WriteSamples(ctx, org, orphan, []telemetry.Sample{
		{Name: "opengate_edge_metric_avg", Value: 9, TS: ts},
	}))
	require.NoError(t, f.vm.Flush(ctx))

	// testvm is one VictoriaMetrics shared across the whole test binary, while each
	// test gets its own Postgres store. Scope the sweep's inventory to this test's
	// org so it neither sweeps a sibling test's series nor treats them as orphans;
	// the production reconciler runs against a single global VM and Postgres.
	inv := &orgScopedInventory{inner: f.vm, org: org}
	rec := NewReconciler(inv, f.vm, NewPostgresPurger(f.store.DB()), nil)
	purged, err := rec.Sweep(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, purged, 1, "the orphan must be swept")

	// The orphan's series are gone.
	n, err := f.vm.CountSeries(ctx, org, &orphan)
	require.NoError(t, err)
	assert.Zero(t, n, "orphan series must be purged")

	// The live device keeps its series and Postgres row.
	n, err = f.vm.CountSeries(ctx, org, &live)
	require.NoError(t, err)
	assert.Positive(t, n, "a device with a Postgres row must not be swept")
	assert.Positive(t, countRows(t, f, dbtx.WithTenant(ctx, org, true), qDevices, live))

	// A second sweep over the now-clean store deletes nothing.
	purged, err = rec.Sweep(ctx)
	require.NoError(t, err)
	assert.Zero(t, purged, "reconcile is idempotent")
}

// orgScopedInventory filters a real inventory down to a single org, isolating a
// sweep from other tests sharing the same VictoriaMetrics instance.
type orgScopedInventory struct {
	inner SubjectLister
	org   uuid.UUID
}

func (o *orgScopedInventory) ListSubjects(ctx context.Context) ([]telemetry.SeriesSubject, error) {
	all, err := o.inner.ListSubjects(ctx)
	if err != nil {
		return nil, err
	}
	var out []telemetry.SeriesSubject
	for _, s := range all {
		if s.OrgID == o.org {
			out = append(out, s)
		}
	}
	return out, nil
}
