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

func sweeperOver(repo session.Repository, live ...string) *session.Sweeper {
	return session.NewSweeper(repo, func() []string { return live }, time.Minute, slog.Default())
}

func deleteStaleAfterNow(t *testing.T, repo session.Repository, keep []string) int {
	t.Helper()
	deleted, err := repo.DeleteStale(context.Background(), time.Now().Add(time.Hour), keep)
	require.NoError(t, err)
	return deleted
}

func TestPostgres_DeleteStale(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := testutil.SeedUser(t, ctx, store)
	site := testutil.SeedSite(t, ctx, store)
	dev := testutil.SeedDevice(t, ctx, store, site.ID)

	create := func(prefix string) string {
		s := &session.Session{Token: prefix + uuid.New().String(), DeviceID: dev.ID, UserID: owner.ID}
		require.NoError(t, repo.Create(ctx, s))
		return s.Token
	}
	orphan, live, alsoOrphan := create("orphan-"), create("live-"), create("orphan2-")

	deleted, err := repo.DeleteStale(context.Background(), time.Now().Add(-time.Hour), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)

	assert.Equal(t, 2, deleteStaleAfterNow(t, repo, []string{live}))

	got, err := repo.Get(ctx, live)
	require.NoError(t, err)
	assert.Equal(t, live, got.Token)
	for _, token := range []string{orphan, alsoOrphan} {
		_, err := repo.Get(ctx, token)
		assert.ErrorIs(t, err, session.ErrSessionNotFound)
	}

	assert.Equal(t, 0, deleteStaleAfterNow(t, repo, []string{live}))
}

func TestPostgres_DeleteStale_CrossTenant(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, true)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	userB := testutil.SeedUser(t, ctxB, store)
	groupB := testutil.SeedSite(t, ctxB, store)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)
	sessionB := testutil.SeedAgentSession(t, ctxB, store, deviceB.ID, userB.ID)

	assert.GreaterOrEqual(t, deleteStaleAfterNow(t, repo, nil), 1)

	_, err := repo.Get(ctxB, sessionB.Token)
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

func TestSweeper_SparesLiveTokensPastTheGrace(t *testing.T) {
	t.Parallel()
	repo := &memRepo{staleDeleted: 2}
	sweeper := sweeperOver(repo, "live-1", "live-2")

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

func TestSweeper_NoLiveSessionsSweepsEverythingPastTheGrace(t *testing.T) {
	t.Parallel()
	repo := &memRepo{staleDeleted: 7}
	sweeper := sweeperOver(repo)

	deleted, err := sweeper.Sweep(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 7, deleted)
	require.Len(t, repo.staleCalls, 1)
	assert.Empty(t, repo.staleCalls[0].keep)
}

func TestSweeper_PropagatesRepositoryFailure(t *testing.T) {
	t.Parallel()
	repo := &memRepo{staleErr: sql.ErrConnDone}
	sweeper := sweeperOver(repo)

	deleted, err := sweeper.Sweep(context.Background())
	require.ErrorIs(t, err, sql.ErrConnDone)
	assert.Equal(t, 0, deleted)
}
