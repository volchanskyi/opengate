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
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// orchestratorFixture wires the real stores, a real VictoriaMetrics client, and
// a real Postgres purger over a throwaway pg + VM.
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
		PG:         NewPostgresPurger(store.DB()),
		Verify:     VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})
	return &orchestratorFixture{store: store, vm: vm, orch: orch, tombstone: tomb, jobs: jobs}
}

// newSeededPurge builds an orchestrator fixture with one device (plus process,
// inventory, and VM telemetry) in a fresh org — the common start of the
// device-purge tests.
func newSeededPurge(t *testing.T) (*orchestratorFixture, context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()
	f := newOrchestratorFixture(t)
	org := uuid.New()
	return f, context.Background(), org, seedDeviceWithTelemetry(t, f, org)
}

// assertJobComplete asserts a purge job reached verified terminal completion.
func assertJobComplete(t *testing.T, f *orchestratorFixture, ctx context.Context, id uuid.UUID) {
	t.Helper()
	got, err := f.jobs.GetJob(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StateComplete, got.State)
	require.NotNil(t, got.CompletedAt, "a complete job is stamped")
}

// seedDeviceWithTelemetry seeds a device in the given org plus process,
// inventory, and VM rows so a purge has something to erase. Returns the org and
// device ids.
func seedDeviceWithTelemetry(t *testing.T, f *orchestratorFixture, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := dbtx.WithTenant(context.Background(), orgID, false)
	testutil.EnsureOrganization(t, ctx, f.store, orgID, "Org "+orgID.String()[:8])
	owner := testutil.SeedUser(t, ctx, f.store)
	group := testutil.SeedGroup(t, ctx, f.store, owner.ID)
	device := testutil.SeedDevice(t, ctx, f.store, group.ID)

	ts := time.Now().UTC().Truncate(time.Second)
	procs := telemetry.NewPostgresProcessRepository(f.store.DB())
	require.NoError(t, procs.UpsertReport(ctx, device.ID, ts, []telemetry.ProcessSample{
		{Rank: 0, Basename: "sshd", PID: 1, CPU: 1, Mem: 1},
	}))
	inv := inventory.NewPostgresInventoryRepository(f.store.DB())
	require.NoError(t, inv.Replace(ctx, device.ID, ts, []inventory.Component{
		{Kind: inventory.KindPort, Name: "sshd", Proto: "tcp", Port: 22},
	}))
	require.NoError(t, f.vm.WriteSamples(context.Background(), orgID, device.ID, []telemetry.Sample{
		{Name: "opengate_edge_metric_avg", Value: 5, TS: ts, Labels: map[string]string{"dim": "cpu"}},
	}))
	require.NoError(t, f.vm.Flush(context.Background()))
	return device.ID
}

func TestOrchestratorPurgeDeviceFansOutAndVerifies(t *testing.T) {
	t.Parallel()
	f, ctx, org, device := newSeededPurge(t)

	job, err := f.orch.PurgeDevice(ctx, org, device, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(ctx, job))

	// Job reached verified completion across every store.
	assertJobComplete(t, f, ctx, job.ID)
	got, err := f.jobs.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.True(t, got.Verified && got.VMDeleted && got.PGDeleted)

	// Tombstone blocks future ingest.
	tombstoned, err := f.tombstone.IsDeviceTombstoned(ctx, org, device)
	require.NoError(t, err)
	assert.True(t, tombstoned)

	// VM series gone.
	n, err := f.vm.CountSeries(ctx, org, &device)
	require.NoError(t, err)
	assert.Zero(t, n)

	// Postgres device row + cascaded telemetry gone.
	scoped := dbtx.WithTenant(ctx, org, true)
	assert.Zero(t, countRows(t, f, scoped, qDevices, device))
	assert.Zero(t, countRows(t, f, scoped, qProcesses, device))
	assert.Zero(t, countRows(t, f, scoped, qInventory, device))
}

func TestOrchestratorPurgeDeviceIsIdempotent(t *testing.T) {
	t.Parallel()
	f, ctx, org, device := newSeededPurge(t)

	job, err := f.orch.PurgeDevice(ctx, org, device, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(ctx, job))
	// Running the same completed job again must not error.
	require.NoError(t, f.orch.Run(ctx, job))
}

func TestOrchestratorResumesAfterMidPurgeCrash(t *testing.T) {
	t.Parallel()
	f, ctx, org, device := newSeededPurge(t)

	// Simulate a crash after the tombstone + VM delete but before Postgres delete:
	// a purger that fails once, wrapping the real one.
	flaky := &flakyPGPurger{inner: NewPostgresPurger(f.store.DB()), failuresLeft: 1}
	crashOrch := NewOrchestrator(OrchestratorConfig{
		Tombstones: f.tombstone, Jobs: f.jobs, Series: f.vm, PG: flaky,
		Verify: VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})

	job, err := crashOrch.PurgeDevice(ctx, org, device, nil)
	require.NoError(t, err)
	require.Error(t, crashOrch.Run(ctx, job), "postgres delete fails mid-purge")

	// The crash left the subject marked deleted (tombstone + VM already gone), not
	// half-alive: VM is empty but the device row still exists.
	n, err := f.vm.CountSeries(ctx, org, &device)
	require.NoError(t, err)
	assert.Zero(t, n, "VM delete already issued before the crash")

	// Resume re-runs the incomplete job to completion.
	require.NoError(t, crashOrch.Resume(ctx))
	assertJobComplete(t, f, ctx, job.ID)
	assert.Zero(t, countRows(t, f, dbtx.WithTenant(ctx, org, true), qDevices, device))
}

func TestOrchestratorPurgeOrgLeavesOtherTenantsUntouched(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	ctx := context.Background()

	orgA := uuid.New()
	orgB := uuid.New()
	deviceA1 := seedDeviceWithTelemetry(t, f, orgA)
	deviceA2 := seedDeviceWithTelemetry(t, f, orgA)
	deviceB := seedDeviceWithTelemetry(t, f, orgB)

	job, err := f.orch.PurgeOrg(ctx, orgA, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(ctx, job))
	assertJobComplete(t, f, ctx, job.ID)

	// Every orgA device is gone from VM and Postgres.
	nA, err := f.vm.CountSeries(ctx, orgA, nil)
	require.NoError(t, err)
	assert.Zero(t, nA)
	for _, d := range []uuid.UUID{deviceA1, deviceA2} {
		assert.Zero(t, countRows(t, f, dbtx.WithTenant(ctx, orgA, true), qDevices, d))
	}

	// orgB is fully intact.
	nB, err := f.vm.CountSeries(ctx, orgB, nil)
	require.NoError(t, err)
	assert.Positive(t, nB)
	assert.Positive(t, countRows(t, f, dbtx.WithTenant(ctx, orgB, true), qDevices, deviceB))
	tombstoned, err := f.tombstone.IsDeviceTombstoned(ctx, orgB, deviceB)
	require.NoError(t, err)
	assert.False(t, tombstoned, "org purge must not tombstone another tenant")
}

// flakyPGPurger wraps a real PGPurger and fails DeleteDevice a fixed number of
// times to simulate a mid-purge crash.
type flakyPGPurger struct {
	inner        PGPurger
	failuresLeft int
}

func (f *flakyPGPurger) DeleteDevice(ctx context.Context, orgID, deviceID uuid.UUID) error {
	if f.failuresLeft > 0 {
		f.failuresLeft--
		return errors.New("simulated crash")
	}
	return f.inner.DeleteDevice(ctx, orgID, deviceID)
}

func (f *flakyPGPurger) DeleteOrgDevices(ctx context.Context, orgID uuid.UUID) (int, error) {
	return f.inner.DeleteOrgDevices(ctx, orgID)
}

func (f *flakyPGPurger) ListOrgDeviceIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	return f.inner.ListOrgDeviceIDs(ctx, orgID)
}

func (f *flakyPGPurger) ListAllDeviceIDs(ctx context.Context) ([]uuid.UUID, error) {
	return f.inner.ListAllDeviceIDs(ctx)
}

// Fixed count queries per table, so the test never string-builds SQL.
const (
	qDevices   = `SELECT COUNT(*) FROM devices WHERE id = $1`
	qProcesses = `SELECT COUNT(*) FROM device_processes WHERE device_id = $1`
	qInventory = `SELECT COUNT(*) FROM device_inventory WHERE device_id = $1`
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
