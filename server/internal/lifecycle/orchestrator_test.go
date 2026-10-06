package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

type orchestratorFixture struct {
	store     *db.PostgresStore
	vm        *telemetry.VMClient
	orch      *Orchestrator
	tombstone *TombstoneStore
	jobs      *JobStore
}

func newOrchestratorFixture(t *testing.T) *orchestratorFixture {
	t.Helper()
	store := testutil.NewTestStore(t)
	vm := telemetry.NewVMClient(testvm.BaseURL(t), nil)
	tomb := NewTombstoneStore(store.DB())
	jobs := NewJobStore(store.DB())
	orch := NewOrchestrator(OrchestratorConfig{
		Tombstones: tomb,
		Jobs:       jobs,
		Series:     vm,
		PG:         NewPostgresPurger(store.DB(), alerts.NewStore(store.DB())),
		Verify:     VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})
	return &orchestratorFixture{store: store, vm: vm, orch: orch, tombstone: tomb, jobs: jobs}
}

func newSeededPurge(t *testing.T) (*orchestratorFixture, context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()
	f := newOrchestratorFixture(t)
	tenant := uuid.New()
	return f, context.Background(), tenant, seedDeviceWithTelemetry(t, f, tenant)
}

func assertJobComplete(t *testing.T, f *orchestratorFixture, ctx context.Context, id uuid.UUID) {
	t.Helper()
	got, err := f.jobs.GetJob(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StateComplete, got.State)
	require.NotNil(t, got.CompletedAt, "a complete job is stamped")
}

func assertNoSeries(t *testing.T, f *orchestratorFixture, ctx context.Context, tenant uuid.UUID, device *uuid.UUID, msgAndArgs ...any) {
	t.Helper()
	n, err := f.vm.CountSeries(ctx, tenant, device)
	require.NoError(t, err)
	assert.Zero(t, n, msgAndArgs...)
}

func assertTombstoned(t *testing.T, f *orchestratorFixture, ctx context.Context, tenant, device uuid.UUID, want bool, msgAndArgs ...any) {
	t.Helper()
	tombstoned, err := f.tombstone.IsDeviceTombstoned(ctx, tenant, device)
	require.NoError(t, err)
	assert.Equal(t, want, tombstoned, msgAndArgs...)
}

func purgeDeviceAndRun(t *testing.T, f *orchestratorFixture, ctx context.Context, tenant, device uuid.UUID) *PurgeJob {
	t.Helper()
	job, err := f.orch.PurgeDevice(ctx, tenant, device, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(ctx, job))
	return job
}

func seedDeviceWithTelemetry(t *testing.T, f *orchestratorFixture, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)
	testutil.EnsureTenant(t, ctx, f.store, tenantID, "Tenant "+tenantID.String()[:8])
	site := testutil.SeedSite(t, ctx, f.store)
	device := testutil.SeedDevice(t, ctx, f.store, site.ID)

	ts := time.Now().UTC().Truncate(time.Second)
	procs := telemetry.NewPostgresProcessRepository(f.store.DB())
	require.NoError(t, procs.UpsertReport(ctx, device.ID, ts, []telemetry.ProcessSample{
		{Rank: 0, Basename: "sshd", PID: 1, CPU: 1, Mem: 1},
	}))
	inv := inventory.NewPostgresInventoryRepository(f.store.DB())
	require.NoError(t, inv.Replace(ctx, device.ID, ts, []inventory.Component{
		{Kind: inventory.KindPort, Name: "sshd", Proto: "tcp", Port: 22},
	}))
	// A machine that cannot evaluate a rule carries a standing coverage row.
	require.NoError(t, rules.NewStore(f.store.DB()).MarkUnsupported(ctx, site.OrganizationID, device.ID, "io-stalled"))
	require.NoError(t, f.vm.WriteSamples(context.Background(), tenantID, device.ID, []telemetry.Sample{
		{Name: "opengate_edge_metric_avg", Value: 5, TS: ts, Labels: map[string]string{"dim": "cpu"}},
	}))
	require.NoError(t, f.vm.Flush(context.Background()))
	return device.ID
}

func TestOrchestratorPurgeDeviceFansOutAndVerifies(t *testing.T) {
	t.Parallel()
	f, ctx, tenant, device := newSeededPurge(t)

	job := purgeDeviceAndRun(t, f, ctx, tenant, device)

	assertJobComplete(t, f, ctx, job.ID)
	got, err := f.jobs.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.True(t, got.Verified && got.VMDeleted && got.PGDeleted)

	assertTombstoned(t, f, ctx, tenant, device, true)
	assertNoSeries(t, f, ctx, tenant, &device)

	scoped := dbtx.WithTenant(ctx, tenant, true)
	assert.Zero(t, countRows(t, f, scoped, qDevices, device))
	assert.Zero(t, countRows(t, f, scoped, qProcesses, device))
	assert.Zero(t, countRows(t, f, scoped, qInventory, device))
	assert.Zero(t, countRows(t, f, scoped, qCoverage, device),
		"a decommissioned machine must stop counting against a rule's coverage")
}

func TestOrchestratorPurgeDeviceIsIdempotent(t *testing.T) {
	t.Parallel()
	f, ctx, tenant, device := newSeededPurge(t)

	job := purgeDeviceAndRun(t, f, ctx, tenant, device)
	require.NoError(t, f.orch.Run(ctx, job))
}

func TestOrchestratorResumesAfterMidPurgeCrash(t *testing.T) {
	t.Parallel()
	f, ctx, tenant, device := newSeededPurge(t)

	flaky := &flakyPGPurger{inner: NewPostgresPurger(f.store.DB(), alerts.NewStore(f.store.DB())), failuresLeft: 1}
	crashOrch := NewOrchestrator(OrchestratorConfig{
		Tombstones: f.tombstone, Jobs: f.jobs, Series: f.vm, PG: flaky,
		Verify: VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})

	job, err := crashOrch.PurgeDevice(ctx, tenant, device, nil)
	require.NoError(t, err)
	require.Error(t, crashOrch.Run(ctx, job), "postgres delete fails mid-purge")

	assertNoSeries(t, f, ctx, tenant, &device, "VM delete already issued before the crash")

	require.NoError(t, crashOrch.Resume(ctx))
	assertJobComplete(t, f, ctx, job.ID)
	assert.Zero(t, countRows(t, f, dbtx.WithTenant(ctx, tenant, true), qDevices, device))
}

func TestOrchestratorPurgeTenantLeavesOtherTenantsUntouched(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()
	deviceA1 := seedDeviceWithTelemetry(t, f, tenantA)
	deviceA2 := seedDeviceWithTelemetry(t, f, tenantA)
	deviceB := seedDeviceWithTelemetry(t, f, tenantB)

	job, err := f.orch.PurgeTenant(ctx, tenantA, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(ctx, job))
	assertJobComplete(t, f, ctx, job.ID)

	assertNoSeries(t, f, ctx, tenantA, nil)
	for _, d := range []uuid.UUID{deviceA1, deviceA2} {
		assert.Zero(t, countRows(t, f, dbtx.WithTenant(ctx, tenantA, true), qDevices, d))
	}

	nB, err := f.vm.CountSeries(ctx, tenantB, nil)
	require.NoError(t, err)
	assert.Positive(t, nB)
	assert.Positive(t, countRows(t, f, dbtx.WithTenant(ctx, tenantB, true), qDevices, deviceB))
	assertTombstoned(t, f, ctx, tenantB, deviceB, false, "tenant purge must not tombstone another tenant")
}

type flakyPGPurger struct {
	inner        PGPurger
	failuresLeft int
}

func (f *flakyPGPurger) DeleteDevice(ctx context.Context, tenantID, deviceID uuid.UUID) error {
	if f.failuresLeft > 0 {
		f.failuresLeft--
		return errors.New("simulated crash")
	}
	return f.inner.DeleteDevice(ctx, tenantID, deviceID)
}

func (f *flakyPGPurger) DeleteTenantDevices(ctx context.Context, tenantID uuid.UUID) (int, error) {
	return f.inner.DeleteTenantDevices(ctx, tenantID)
}

func (f *flakyPGPurger) ListTenantDeviceIDs(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	return f.inner.ListTenantDeviceIDs(ctx, tenantID)
}

func (f *flakyPGPurger) ListAllDeviceIDs(ctx context.Context) ([]uuid.UUID, error) {
	return f.inner.ListAllDeviceIDs(ctx)
}

// Fixed count queries per table, so the test never string-builds SQL.
const (
	qDevices   = `SELECT COUNT(*) FROM devices WHERE id = $1`
	qProcesses = `SELECT COUNT(*) FROM device_processes WHERE device_id = $1`
	qInventory = `SELECT COUNT(*) FROM device_inventory WHERE device_id = $1`
	qCoverage  = `SELECT COUNT(*) FROM rule_coverage_unsupported WHERE device_id = $1`
)

func countRows(t *testing.T, f *orchestratorFixture, ctx context.Context, query string, device uuid.UUID) int {
	t.Helper()
	var n int
	err := dbtx.Scoped(ctx, f.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, query, device).Scan(&n)
	})
	require.NoError(t, err)
	return n
}
