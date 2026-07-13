package lifecycle

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newJobFixture returns a fresh job store over a throwaway Postgres plus a
// background context, the common start of every job-store test.
func newJobFixture(t *testing.T) (*JobStore, context.Context) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return NewJobStore(store.DB()), context.Background()
}

func TestJobStoreCreateGetRoundTrips(t *testing.T) {
	t.Parallel()
	js, ctx := newJobFixture(t)

	device := uuid.New()
	by := uuid.New()
	job := &PurgeJob{
		ID:          uuid.New(),
		OrgID:       uuid.New(),
		DeviceID:    &device,
		Scope:       ScopeDevice,
		State:       StateRequested,
		RequestedBy: &by,
	}
	require.NoError(t, js.CreateJob(ctx, job))

	got, err := js.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.OrgID, got.OrgID)
	require.NotNil(t, got.DeviceID)
	assert.Equal(t, device, *got.DeviceID)
	assert.Equal(t, ScopeDevice, got.Scope)
	assert.Equal(t, StateRequested, got.State)
	require.NotNil(t, got.RequestedBy)
	assert.Equal(t, by, *got.RequestedBy)
	assert.Nil(t, got.CompletedAt)
}

func TestJobStoreUpdatePersistsProgress(t *testing.T) {
	t.Parallel()
	js, ctx := newJobFixture(t)

	job := &PurgeJob{ID: uuid.New(), OrgID: uuid.New(), Scope: ScopeOrg, State: StateRequested}
	require.NoError(t, js.CreateJob(ctx, job))

	job.State = StateComplete
	job.VMDeleted = true
	job.PGDeleted = true
	job.Verified = true
	require.NoError(t, js.MarkComplete(ctx, job))

	got, err := js.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, StateComplete, got.State)
	assert.True(t, got.VMDeleted)
	assert.True(t, got.PGDeleted)
	assert.True(t, got.Verified)
	require.NotNil(t, got.CompletedAt, "completing a job stamps completed_at")
}

func TestJobStoreListIncompleteExcludesCompleted(t *testing.T) {
	t.Parallel()
	js, ctx := newJobFixture(t)

	org := uuid.New()
	done := &PurgeJob{ID: uuid.New(), OrgID: org, Scope: ScopeOrg, State: StateRequested}
	pending := &PurgeJob{ID: uuid.New(), OrgID: org, Scope: ScopeOrg, State: StateRequested}
	require.NoError(t, js.CreateJob(ctx, done))
	require.NoError(t, js.CreateJob(ctx, pending))
	require.NoError(t, js.MarkComplete(ctx, done))

	incomplete, err := js.ListIncomplete(ctx)
	require.NoError(t, err)

	var ids []uuid.UUID
	for _, j := range incomplete {
		ids = append(ids, j.ID)
	}
	assert.Contains(t, ids, pending.ID, "an unfinished job must be resumable")
	assert.NotContains(t, ids, done.ID, "a completed job must not resume")
}

func TestJobStoreUpdateProgressPersistsError(t *testing.T) {
	t.Parallel()
	js, ctx := newJobFixture(t)

	job := &PurgeJob{ID: uuid.New(), OrgID: uuid.New(), Scope: ScopeDevice, State: StateRequested}
	require.NoError(t, js.CreateJob(ctx, job))

	job.State = StateCentralPhysicalPending
	job.VMDeleted = true
	job.LastError = "verify pending"
	require.NoError(t, js.UpdateProgress(ctx, job))

	got, err := js.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, StateCentralPhysicalPending, got.State)
	assert.True(t, got.VMDeleted)
	assert.Equal(t, "verify pending", got.LastError)
	assert.Nil(t, got.CompletedAt, "a still-running job has no completion stamp")
}

func TestJobStoreLatestForSubject(t *testing.T) {
	t.Parallel()
	js, ctx := newJobFixture(t)

	org := uuid.New()
	device := uuid.New()
	job := &PurgeJob{ID: uuid.New(), OrgID: org, DeviceID: &device, Scope: ScopeDevice, State: StateRequested}
	require.NoError(t, js.CreateJob(ctx, job))

	got, err := js.LatestForOrg(ctx, org)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)

	// An org with no purge job returns nil, not an error.
	none, err := js.LatestForOrg(ctx, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, none)
}
