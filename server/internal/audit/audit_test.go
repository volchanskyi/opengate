package audit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// newTestRepo returns a Postgres-backed audit.Repository against the per-test
// isolated schema created by testutil.NewTestStore. Tests using this MAY call
// t.Parallel(); each gets its own schema.
func newTestRepo(t *testing.T) audit.Repository {
	t.Helper()
	store := testutil.NewTestStore(t)
	return testutil.NewTestAudit(t, store)
}

func TestPostgres_WriteAndQuery(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()
	userID := uuid.New()

	t.Run("write and query by user", func(t *testing.T) {
		event := &audit.Event{
			UserID:  userID,
			Action:  "login",
			Target:  "session",
			Details: "from 10.0.0.1",
		}
		require.NoError(t, repo.Write(ctx, event))

		events, err := repo.Query(ctx, audit.Query{UserID: &userID, Limit: 10})
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "login", events[0].Action)
		assert.Equal(t, "from 10.0.0.1", events[0].Details)
		assert.False(t, events[0].CreatedAt.IsZero())
		assert.Greater(t, events[0].ID, int64(0))
	})

	t.Run("query by action", func(t *testing.T) {
		require.NoError(t, repo.Write(ctx, &audit.Event{UserID: userID, Action: "logout"}))
		require.NoError(t, repo.Write(ctx, &audit.Event{UserID: userID, Action: "login"}))

		events, err := repo.Query(ctx, audit.Query{Action: "logout", Limit: 100})
		require.NoError(t, err)
		for _, e := range events {
			assert.Equal(t, "logout", e.Action)
		}
	})

	t.Run("query with limit and offset", func(t *testing.T) {
		paginateUser := uuid.New()
		for range 5 {
			require.NoError(t, repo.Write(ctx, &audit.Event{UserID: paginateUser, Action: "paginate"}))
		}

		page1, err := repo.Query(ctx, audit.Query{UserID: &paginateUser, Action: "paginate", Limit: 2})
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := repo.Query(ctx, audit.Query{UserID: &paginateUser, Action: "paginate", Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 2)
		assert.NotEqual(t, page1[0].ID, page2[0].ID)
	})

	t.Run("query no filters returns all", func(t *testing.T) {
		events, err := repo.Query(ctx, audit.Query{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(events), 1)
	})
}

// fakeObserver records every Observe call for the Instrumented decorator test.
type fakeObserver struct {
	calls []observerCall
}

type observerCall struct {
	op       string
	duration time.Duration
	ok       bool
}

func (f *fakeObserver) Observe(op string, d time.Duration, ok bool) {
	f.calls = append(f.calls, observerCall{op: op, duration: d, ok: ok})
}

// memRepo is an in-memory Repository for testing the Instrumented decorator
// without needing Postgres.
type memRepo struct {
	writeErr error
	queryErr error
	events   []*audit.Event
}

func (m *memRepo) Write(_ context.Context, e *audit.Event) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.events = append(m.events, e)
	return nil
}

func (m *memRepo) Query(_ context.Context, _ audit.Query) ([]*audit.Event, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.events, nil
}

func TestInstrumented_ObservesWrite(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := audit.NewInstrumented(&memRepo{}, obs)

	require.NoError(t, repo.Write(context.Background(), &audit.Event{Action: "test"}))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "audit.Write", obs.calls[0].op)
	assert.True(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesQueryError(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := audit.NewInstrumented(&memRepo{queryErr: sql.ErrConnDone}, obs)

	_, err := repo.Query(context.Background(), audit.Query{})
	require.Error(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "audit.Query", obs.calls[0].op)
	assert.False(t, obs.calls[0].ok)
}
