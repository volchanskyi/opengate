package session_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestPostgres_SessionCRUD(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	ctx := context.Background()
	owner := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, owner.ID)
	dev := testutil.SeedDevice(t, ctx, store, group.ID)

	t.Run("create and get", func(t *testing.T) {
		s := &session.Session{
			Token:    "tok-" + uuid.New().String()[:8],
			DeviceID: dev.ID,
			UserID:   owner.ID,
		}
		require.NoError(t, repo.Create(ctx, s))

		got, err := repo.Get(ctx, s.Token)
		require.NoError(t, err)
		assert.Equal(t, s.Token, got.Token)
		assert.Equal(t, dev.ID, got.DeviceID)
		assert.Equal(t, owner.ID, got.UserID)
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, "nonexistent")
		assert.True(t, errors.Is(err, session.ErrSessionNotFound))
	})

	t.Run("list active for device", func(t *testing.T) {
		device2 := testutil.SeedDevice(t, ctx, store, group.ID)
		s1 := &session.Session{Token: "s1-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		s2 := &session.Session{Token: "s2-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		require.NoError(t, repo.Create(ctx, s1))
		require.NoError(t, repo.Create(ctx, s2))

		sessions, err := repo.ListActiveForDevice(ctx, device2.ID)
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
	})

	t.Run("delete", func(t *testing.T) {
		s := &session.Session{Token: "del-" + uuid.New().String()[:8], DeviceID: dev.ID, UserID: owner.ID}
		require.NoError(t, repo.Create(ctx, s))
		require.NoError(t, repo.Delete(ctx, s.Token))
		_, err := repo.Get(ctx, s.Token)
		assert.True(t, errors.Is(err, session.ErrSessionNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, "nope")
		assert.True(t, errors.Is(err, session.ErrSessionNotFound))
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

// memRepo is an in-memory session.Repository for testing the Instrumented decorator.
type memRepo struct {
	createErr error
	getErr    error
	listErr   error
	deleteErr error
	sessions  map[string]*session.Session
}

func (m *memRepo) Create(_ context.Context, s *session.Session) error {
	if m.createErr != nil {
		return m.createErr
	}
	if m.sessions == nil {
		m.sessions = make(map[string]*session.Session)
	}
	m.sessions[s.Token] = s
	return nil
}

func (m *memRepo) Get(_ context.Context, token string) (*session.Session, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	s, ok := m.sessions[token]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	return s, nil
}

func (m *memRepo) Delete(_ context.Context, _ string) error { return m.deleteErr }

func (m *memRepo) ListActiveForDevice(_ context.Context, _ uuid.UUID) ([]*session.Session, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return nil, nil
}

func TestInstrumented_ObservesCreate(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := session.NewInstrumented(&memRepo{}, obs)

	require.NoError(t, repo.Create(context.Background(), &session.Session{Token: "t1"}))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "session.Create", obs.calls[0].op)
	assert.True(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesGetError(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := session.NewInstrumented(&memRepo{getErr: sql.ErrConnDone}, obs)

	_, err := repo.Get(context.Background(), "tok")
	require.Error(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "session.Get", obs.calls[0].op)
	assert.False(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesListActiveForDevice(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := session.NewInstrumented(&memRepo{}, obs)

	_, err := repo.ListActiveForDevice(context.Background(), uuid.New())
	require.NoError(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "session.ListActiveForDevice", obs.calls[0].op)
}

func TestInstrumented_ObservesDelete(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := session.NewInstrumented(&memRepo{}, obs)

	require.NoError(t, repo.Delete(context.Background(), "tok"))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "session.Delete", obs.calls[0].op)
}
