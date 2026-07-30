package session_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestPostgres_DeleteStale covers the garbage collection behind the device
// page's "Active Sessions" list. A session row outlives its relay whenever the
// pair never formed or the process that owned it died, so the sweep drops every
// row past the cutoff except the tokens the relay still holds open.
func TestPostgres_DeleteStale(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store)
	dev := testutil.SeedDevice(t, ctx, store, group.ID)

	create := func(prefix string) string {
		s := &session.Session{Token: prefix + uuid.New().String(), DeviceID: dev.ID, UserID: owner.ID}
		require.NoError(t, repo.Create(ctx, s))
		return s.Token
	}
	orphan, live, alsoOrphan := create("orphan-"), create("live-"), create("orphan2-")

	// A cutoff in the past spares everything: the rows are all newer than it.
	deleted, err := repo.DeleteStale(context.Background(), time.Now().Add(-time.Hour), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)

	// A cutoff ahead of every row makes them all stale — except the one whose
	// relay is still piping.
	deleted, err = repo.DeleteStale(context.Background(), time.Now().Add(time.Hour), []string{live})
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	got, err := repo.Get(ctx, live)
	require.NoError(t, err)
	assert.Equal(t, live, got.Token)
	for _, token := range []string{orphan, alsoOrphan} {
		_, err := repo.Get(ctx, token)
		assert.ErrorIs(t, err, session.ErrSessionNotFound)
	}

	// Idempotent: a second sweep over the same state deletes nothing.
	deleted, err = repo.DeleteStale(context.Background(), time.Now().Add(time.Hour), []string{live})
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)
}

// TestPostgres_DeleteStale_CrossOrg pins the sweep as fleet-wide: it runs
// without a request tenant, so it must reach every organization's rows.
func TestPostgres_DeleteStale_CrossOrg(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	orgB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), orgB, true)
	testutil.EnsureOrganization(t, context.Background(), store, orgB, "Tenant "+orgB.String()[:8])

	userB := testutil.SeedUser(t, ctxB, store)
	groupB := testutil.SeedGroup(t, ctxB, store)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)
	sessionB := testutil.SeedAgentSession(t, ctxB, store, deviceB.ID, userB.ID)

	deleted, err := repo.DeleteStale(context.Background(), time.Now().Add(time.Hour), nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, 1)

	_, err = repo.Get(ctxB, sessionB.Token)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestInstrumented_ObservesDeleteStale(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := session.NewInstrumented(&memRepo{staleDeleted: 3}, obs)

	deleted, err := repo.DeleteStale(context.Background(), time.Now(), []string{"keep"})
	require.NoError(t, err)
	assert.Equal(t, 3, deleted)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "session.DeleteStale", obs.calls[0].op)
	assert.True(t, obs.calls[0].ok)
}

// TestSweeper_SparesLiveTokensPastTheGrace pins the two inputs the sweep
// derives: the cutoff is now minus the grace period, and the keep-list is
// whatever the relay currently holds open.
func TestSweeper_SparesLiveTokensPastTheGrace(t *testing.T) {
	t.Parallel()
	repo := &memRepo{staleDeleted: 2}
	sweeper := session.NewSweeper(repo, func() []string { return []string{"live-1", "live-2"} }, time.Minute, slog.Default())

	before := time.Now()
	deleted, err := sweeper.Sweep(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	require.Len(t, repo.staleCalls, 1)
	call := repo.staleCalls[0]
	assert.Equal(t, []string{"live-1", "live-2"}, call.keep)
	assert.WithinDuration(t, before.Add(-time.Minute), call.cutoff, 5*time.Second)
	assert.True(t, call.cutoff.Before(before), "cutoff must trail now by the grace period")
}

// TestSweeper_NoLiveSessionsSweepsEverythingPastTheGrace is the restart case:
// the relay owns nothing, so every aged row goes.
func TestSweeper_NoLiveSessionsSweepsEverythingPastTheGrace(t *testing.T) {
	t.Parallel()
	repo := &memRepo{staleDeleted: 7}
	sweeper := session.NewSweeper(repo, func() []string { return nil }, time.Minute, slog.Default())

	deleted, err := sweeper.Sweep(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 7, deleted)
	require.Len(t, repo.staleCalls, 1)
	assert.Empty(t, repo.staleCalls[0].keep)
}

// TestSweeper_PropagatesRepositoryFailure keeps a broken sweep loud rather than
// silently reporting a clean run.
func TestSweeper_PropagatesRepositoryFailure(t *testing.T) {
	t.Parallel()
	repo := &memRepo{staleErr: sql.ErrConnDone}
	sweeper := session.NewSweeper(repo, func() []string { return nil }, time.Minute, slog.Default())

	deleted, err := sweeper.Sweep(context.Background())
	require.ErrorIs(t, err, sql.ErrConnDone)
	assert.Equal(t, 0, deleted)
}
