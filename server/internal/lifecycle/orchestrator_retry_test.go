package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// heldSeries counts deletes and holds each one until the test lets it go.
type heldSeries struct {
	SeriesPurger
	deletes atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (h *heldSeries) DeleteSeries(ctx context.Context, tenantID uuid.UUID, deviceID *uuid.UUID) error {
	h.deletes.Add(1)
	h.entered <- struct{}{}
	<-h.release
	return h.SeriesPurger.DeleteSeries(ctx, tenantID, deviceID)
}

func newHeldOrchestrator(t *testing.T) (*orchestratorFixture, *heldSeries, *Orchestrator) {
	t.Helper()
	f := newOrchestratorFixture(t)
	held := &heldSeries{SeriesPurger: f.vm, entered: make(chan struct{}, 4), release: make(chan struct{})}
	orch := NewOrchestrator(OrchestratorConfig{
		Tombstones: f.tombstone, Jobs: f.jobs, Series: held,
		PG:     NewPostgresPurger(f.store.DB(), alerts.NewStore(f.store.DB())),
		Verify: VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})
	return f, held, orch
}

func TestTheRetryLeavesARecordItsCallerIsRunning(t *testing.T) {
	t.Parallel()
	f, held, orch := newHeldOrchestrator(t)
	ctx := context.Background()
	tenant := uuid.New()
	device := seedDeviceWithTelemetry(t, f, tenant)

	job, err := orch.PurgeDevice(ctx, tenant, device, nil)
	require.NoError(t, err)
	ran := make(chan error, 1)
	go func() { ran <- orch.Run(ctx, job) }()
	<-held.entered

	unfinished, err := orch.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, unfinished, "a record still running is still unfinished")
	assert.Equal(t, int32(1), held.deletes.Load(), "the retry leaves the running record to its caller")

	stored, err := f.jobs.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.ErrorIs(t, orch.Run(ctx, stored), errPurgeRunning, "another copy of the record waits its turn")
	assert.Equal(t, int32(1), held.deletes.Load())

	close(held.release)
	require.NoError(t, <-ran)
	assertJobComplete(t, f, ctx, job.ID)
}

func TestTheRetryLeavesARecordCreatedAndNotYetRun(t *testing.T) {
	t.Parallel()
	f, held, orch := newHeldOrchestrator(t)
	close(held.release)
	ctx := context.Background()
	tenant := uuid.New()
	device := seedDeviceWithTelemetry(t, f, tenant)

	job, err := orch.PurgeDevice(ctx, tenant, device, nil)
	require.NoError(t, err)

	unfinished, err := orch.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, unfinished)
	assert.Zero(t, held.deletes.Load(), "the request that created the record runs it")

	require.NoError(t, orch.Run(ctx, job))
	assert.Equal(t, int32(1), held.deletes.Load())
	assertJobComplete(t, f, ctx, job.ID)

	unfinished, err = orch.Resume(ctx)
	require.NoError(t, err)
	assert.Zero(t, unfinished, "nothing is left once the record completes")
}
