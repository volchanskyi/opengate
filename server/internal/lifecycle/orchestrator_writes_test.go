package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// heldWrites holds the erasure's wait for in-flight writes until the test lets it go.
type heldWrites struct {
	waited  chan *uuid.UUID
	release chan error
}

func (h *heldWrites) WaitForWrites(_ context.Context, _ uuid.UUID, device *uuid.UUID) error {
	h.waited <- device
	return <-h.release
}

func TestSeriesAreDeletedOnlyOnceTheWritesBeforeTheEntryHaveFinished(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	held := &heldSeries{SeriesPurger: f.vm, entered: make(chan struct{}, 4), release: make(chan struct{})}
	close(held.release)
	writes := &heldWrites{waited: make(chan *uuid.UUID, 1), release: make(chan error, 1)}
	orch := NewOrchestrator(OrchestratorConfig{
		Tombstones: f.tombstone, Jobs: f.jobs, Series: held, Writes: writes,
		PG:     NewPostgresPurger(f.store.DB(), alerts.NewStore(f.store.DB())),
		Verify: VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})
	ctx := context.Background()
	tenant := uuid.New()
	device := seedDeviceWithTelemetry(t, f, tenant)

	job, err := orch.PurgeDevice(ctx, tenant, device, nil)
	require.NoError(t, err)
	ran := make(chan error, 1)
	go func() { ran <- orch.Run(ctx, job) }()

	waitedFor := <-writes.waited
	require.NotNil(t, waitedFor)
	assert.Equal(t, device, *waitedFor, "the erasure waits for this machine's writes")
	tombstoned, err := f.tombstone.IsDeviceTombstoned(ctx, tenant, device)
	require.NoError(t, err)
	assert.True(t, tombstoned, "the deny-list entry is written before the wait")
	assert.Zero(t, held.deletes.Load(), "no series is deleted while a write may still land")

	writes.release <- nil
	require.NoError(t, <-ran)
	assert.Equal(t, int32(1), held.deletes.Load())
	assertJobComplete(t, f, ctx, job.ID)
}

func TestAnErasureWhoseWaitFailsIsRecordedAndRetried(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	writes := &heldWrites{waited: make(chan *uuid.UUID, 2), release: make(chan error, 2)}
	orch := NewOrchestrator(OrchestratorConfig{
		Tombstones: f.tombstone, Jobs: f.jobs, Series: f.vm, Writes: writes,
		PG:     NewPostgresPurger(f.store.DB(), alerts.NewStore(f.store.DB())),
		Verify: VerifyConfig{MaxAttempts: 20, Interval: 250 * time.Millisecond},
	})
	ctx := context.Background()
	tenant := uuid.New()
	device := seedDeviceWithTelemetry(t, f, tenant)
	job, err := orch.PurgeDevice(ctx, tenant, device, nil)
	require.NoError(t, err)

	writes.release <- errors.New("deadline passed")
	require.Error(t, orch.Run(ctx, job))
	stored, err := f.jobs.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.False(t, stored.VMDeleted, "series stay until the wait completes")
	assert.Contains(t, stored.LastError, "vm-delete")

	writes.release <- nil
	unfinished, err := orch.Resume(ctx)
	require.NoError(t, err)
	assert.Zero(t, unfinished)
}
