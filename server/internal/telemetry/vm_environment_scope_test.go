package telemetry

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAClientWithAnEnvironmentListsCountsAndDeletesOnlyItsOwn(t *testing.T) {
	client, ctx := newTestVMClient(t)
	production := client.WithNamespace("opengate")
	staging := client.WithNamespace("opengate-staging")

	// Both environments run the default tenant, so the tenant alone tells their machines apart.
	tenant := uuid.New()
	ours, theirs := uuid.New(), uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	for writer, device := range map[*VMClient]uuid.UUID{production: ours, staging: theirs} {
		require.NoError(t, writer.WriteSamples(ctx, tenant, device, []Sample{
			{Name: "opengate_edge_metric_avg", Value: 1, TS: ts, Labels: map[string]string{"dim": "cpu"}},
			{Name: MetricNodeAnomalyRate, Value: 0.1, TS: ts},
		}))
	}
	require.NoError(t, client.Flush(ctx))

	subjects, err := production.ListSubjects(ctx)
	require.NoError(t, err)
	assert.Contains(t, subjects, SeriesSubject{TenantID: tenant, DeviceID: ours})
	assert.NotContains(t, subjects, SeriesSubject{TenantID: tenant, DeviceID: theirs},
		"the sweep never sees a machine of the other environment")

	n, err := production.CountSeries(ctx, tenant, &theirs)
	require.NoError(t, err)
	assert.Zero(t, n, "the other environment's machine counts as nothing here")

	require.NoError(t, production.DeleteSeries(ctx, tenant, nil))
	n, err = production.CountSeries(ctx, tenant, &ours)
	require.NoError(t, err)
	assert.Zero(t, n, "a tenant-wide delete empties this environment")
	n, err = staging.CountSeries(ctx, tenant, &theirs)
	require.NoError(t, err)
	assert.Positive(t, n, "and leaves the other environment's readings where they were")

	unscoped, err := client.ListSubjects(ctx)
	require.NoError(t, err)
	assert.Contains(t, unscoped, SeriesSubject{TenantID: tenant, DeviceID: theirs},
		"a client with no environment reads the whole store")
}

func TestAnEnvironmentLeadsEverySelectorItBuilds(t *testing.T) {
	t.Parallel()
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	device := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	scoped, err := NewVMClient("http://127.0.0.1:0", nil).WithNamespace("opengate-staging").subjectSelector(tenant, &device)
	require.NoError(t, err)
	assert.Equal(t, `{namespace="opengate-staging",tenant_id="`+tenant.String()+`",device_id="`+device.String()+`"}`, scoped)
}
