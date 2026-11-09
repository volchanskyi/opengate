package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// environment is one server and its own database, writing to a store it may share.
type environment struct {
	name     string
	store    *db.PostgresStore
	assembly *app.Assembly
	readings *telemetry.VMClient
}

// newEnvironment builds a server named for an environment unique to the test, so other tests
// writing to the same store stay out of its sweep.
func newEnvironment(t *testing.T, prefix, storeURL string) *environment {
	t.Helper()
	cfg := baseConfig(t)
	cfg.VictoriaMetricsURL = storeURL
	cfg.Namespace = prefix + "-" + uuid.NewString()[:8]
	assembly, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)
	return &environment{
		name:     cfg.Namespace,
		store:    cfg.Store,
		assembly: assembly,
		readings: telemetry.NewVMClient(testvm.BaseURL(t), nil).WithNamespace(cfg.Namespace),
	}
}

// machineReporting seeds a machine in the environment's default tenant, as both servers run
// that one tenant, and writes one reading for it stamped with the environment.
func (e *environment) machineReporting(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	machine := testutil.SeedDevice(t, ctx, e.store, testutil.SeedSite(t, ctx, e.store).ID)
	require.NoError(t, e.readings.WriteSamples(ctx, dbtx.DefaultTenantID, machine.ID, []telemetry.Sample{
		{Name: "opengate_edge_metric_avg", Value: 7, TS: time.Now().UTC(), Labels: map[string]string{"dim": "cpu"}},
	}))
	require.NoError(t, e.readings.Flush(ctx))
	return machine.ID
}

func (e *environment) seriesOf(t *testing.T, machine uuid.UUID) int {
	t.Helper()
	n, err := e.readings.CountSeries(context.Background(), dbtx.DefaultTenantID, &machine)
	require.NoError(t, err)
	return n
}

func TestTwoEnvironmentsOnOneStoreKeepEachOthersMachinesThroughTheirSweeps(t *testing.T) {
	t.Parallel()
	store := testvm.BaseURL(t)
	production := newEnvironment(t, "opengate", store)
	staging := newEnvironment(t, "opengate-staging", store)
	ours, theirs := production.machineReporting(t), staging.machineReporting(t)

	for _, env := range []*environment{production, staging} {
		purged, err := env.assembly.Reconciler.Sweep(context.Background())
		require.NoError(t, err)
		assert.Zerof(t, purged, "%s's sweep finds no orphan among the other environment's machines", env.name)
	}
	assert.Positive(t, production.seriesOf(t, ours), "production's machine keeps its readings")
	assert.Positive(t, staging.seriesOf(t, theirs), "staging's machine keeps its readings")

	ctx := context.Background()
	job, err := production.assembly.Purger.PurgeDevice(ctx, dbtx.DefaultTenantID, ours, nil)
	require.NoError(t, err)
	require.NoError(t, production.assembly.Purger.Run(ctx, job))

	whole := telemetry.NewVMClient(store, nil)
	n, err := whole.CountSeries(ctx, dbtx.DefaultTenantID, &ours)
	require.NoError(t, err)
	assert.Zero(t, n, "a machine deleted in one environment is in no environment's readings")
	assert.Positive(t, staging.seriesOf(t, theirs), "and the other environment's machine is untouched")
}

// refusingStore stands in front of the store and refuses its delete endpoint while refusing is set.
func refusingStore(t *testing.T, refusing *atomic.Bool) string {
	t.Helper()
	target, err := url.Parse(testvm.BaseURL(t))
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refusing.Load() && r.URL.Path == "/api/v1/admin/tsdb/delete_series" {
			http.Error(w, "the store is not answering", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestADeletionStoppedAtTheReadingsIsFinishedByTheHourlyRetry(t *testing.T) {
	t.Parallel()
	var refusing atomic.Bool
	refusing.Store(true)
	env := newEnvironment(t, "opengate", refusingStore(t, &refusing))
	machine := env.machineReporting(t)
	ctx := context.Background()

	job, err := env.assembly.Purger.PurgeDevice(ctx, dbtx.DefaultTenantID, machine, nil)
	require.NoError(t, err)
	require.Error(t, env.assembly.Purger.Run(ctx, job), "the deletion stops at the readings")

	running, stop := context.WithCancel(ctx)
	defer stop()
	sched := backgroundSchedule()
	sched.Reconcile = time.Hour
	require.NoError(t, env.assembly.StartBackgroundWorkers(running, sched))

	unfinished := func() float64 { return promtestutil.ToFloat64(env.assembly.Metrics.PurgeJobsUnfinished) }
	require.Eventually(t, func() bool { return unfinished() == 1 }, 10*time.Second, 20*time.Millisecond,
		"the stopped deletion reads as unfinished while the store refuses")

	refusing.Store(false)
	require.Eventually(t, func() bool { return unfinished() == 0 }, 30*time.Second, 20*time.Millisecond,
		"the retry finishes the deletion once the store answers")

	got, err := env.assembly.PurgeJobs.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, lifecycle.StateComplete, got.State)
	_, err = testutil.NewTestDevices(t, env.store).Get(dbtx.WithDefaultTenant(ctx, false), machine)
	assert.ErrorIs(t, err, device.ErrDeviceNotFound, "the machine's database rows are gone")
	assert.Zero(t, env.seriesOf(t, machine), "and so are its readings")
}
