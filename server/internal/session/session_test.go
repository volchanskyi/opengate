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
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestPostgres_SessionCRUD(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	owner := testutil.SeedUser(t, ctx, store)
	site := testutil.SeedSite(t, ctx, store)
	dev := testutil.SeedDevice(t, ctx, store, site.ID)

	t.Run("create and get", func(t *testing.T) {
		s := &session.Session{
			Token:    "tok-" + uuid.New().String()[:8],
			DeviceID: dev.ID,
			UserID:   owner.ID,
		}
		replaced, err := repo.Create(ctx, s)
		require.NoError(t, err)
		assert.Empty(t, replaced, "a free host replaces nothing")

		got, err := repo.Get(ctx, s.Token)
		require.NoError(t, err)
		assert.Equal(t, s.Token, got.Token)
		assert.Equal(t, dev.ID, got.DeviceID)
		assert.Equal(t, owner.ID, got.UserID)
		assert.Equal(t, owner.DisplayName, got.HolderName)
		assert.False(t, got.CreatedAt.IsZero())
		require.NoError(t, repo.Delete(ctx, s.Token))
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, "nonexistent")
		assert.True(t, errors.Is(err, session.ErrSessionNotFound))
	})

	t.Run("a host holds one session and only its holder replaces it", func(t *testing.T) {
		device2 := testutil.SeedDevice(t, ctx, store, site.ID)
		other := testutil.SeedUser(t, ctx, store)
		s1 := &session.Session{Token: "s1-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		_, err := repo.Create(ctx, s1)
		require.NoError(t, err)

		_, err = repo.Create(ctx, &session.Session{
			Token: "s2-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: other.ID,
		})
		require.ErrorIs(t, err, session.ErrDeviceHeld, "another technician is refused")

		s3 := &session.Session{Token: "s3-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		replaced, err := repo.Create(ctx, s3)
		require.NoError(t, err)
		assert.Equal(t, s1.Token, replaced, "the holder's new session replaces their old one")
		_, err = repo.Get(ctx, s1.Token)
		assert.ErrorIs(t, err, session.ErrSessionNotFound)

		sessions, err := repo.ListActiveForDevice(ctx, device2.ID)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		assert.Equal(t, s3.Token, sessions[0].Token)
		assert.Equal(t, owner.DisplayName, sessions[0].HolderName)

		held, err := repo.ForDevice(ctx, device2.ID)
		require.NoError(t, err)
		assert.Equal(t, s3.Token, held.Token)

		_, err = repo.ForDevice(ctx, dev.ID)
		assert.ErrorIs(t, err, session.ErrSessionNotFound, "a free host has no session")
	})

	t.Run("each outcome is marked once per session", func(t *testing.T) {
		device3 := testutil.SeedDevice(t, ctx, store, site.ID)
		s := &session.Session{Token: "out-" + uuid.New().String()[:8], DeviceID: device3.ID, UserID: owner.ID}
		_, err := repo.Create(ctx, s)
		require.NoError(t, err)

		first, err := repo.MarkOutcome(ctx, s.Token, 0)
		require.NoError(t, err)
		assert.True(t, first)
		again, err := repo.MarkOutcome(ctx, s.Token, 0)
		require.NoError(t, err)
		assert.False(t, again, "an outcome already marked is not marked twice")
		other, err := repo.MarkOutcome(ctx, s.Token, 3)
		require.NoError(t, err)
		assert.True(t, other, "each outcome has its own mark")

		_, err = repo.MarkOutcome(ctx, "missing", 0)
		assert.ErrorIs(t, err, session.ErrSessionNotFound)
	})

	t.Run("delete", func(t *testing.T) {
		device4 := testutil.SeedDevice(t, ctx, store, site.ID)
		s := &session.Session{Token: "del-" + uuid.New().String()[:8], DeviceID: device4.ID, UserID: owner.ID}
		_, err := repo.Create(ctx, s)
		require.NoError(t, err)
		require.NoError(t, repo.Delete(ctx, s.Token))
		_, err = repo.Get(ctx, s.Token)
		assert.True(t, errors.Is(err, session.ErrSessionNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, "nope")
		assert.True(t, errors.Is(err, session.ErrSessionNotFound))
	})
}

func TestPostgresSessions_TenantDeny(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	tenantB := uuid.New()
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	userA := testutil.SeedUser(t, ctxA, store)
	userB := testutil.SeedUser(t, ctxB, store)
	groupA := testutil.SeedSite(t, ctxA, store)
	groupB := testutil.SeedSite(t, ctxB, store)
	deviceA := testutil.SeedDevice(t, ctxA, store, groupA.ID)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)
	sessionA := testutil.SeedAgentSession(t, ctxA, store, deviceA.ID, userA.ID)
	sessionB := testutil.SeedAgentSession(t, ctxB, store, deviceB.ID, userB.ID)

	_, err := repo.Get(ctxA, sessionB.Token)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
	active, err := repo.ListActiveForDevice(ctxA, deviceB.ID)
	require.NoError(t, err)
	assert.Empty(t, active)
	active, err = repo.ListActiveForDevice(ctxA, deviceA.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, sessionA.Token, active[0].Token)

	_, err = repo.Get(context.Background(), sessionA.Token)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
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

// memRepo is an in-memory session.Repository for testing the Instrumented
// decorator and the Sweeper.
type memRepo struct {
	createErr      error
	getErr         error
	listErr        error
	deleteErr      error
	relayDeleteErr error
	staleErr       error
	sessions       map[string]*session.Session

	// staleCalls records every DeleteStale argument pair, in order.
	staleCalls []staleCall
	// staleDeleted is the count DeleteStale reports.
	staleDeleted int
}

type staleCall struct {
	cutoff time.Time
	keep   []string
}

func (m *memRepo) Create(_ context.Context, s *session.Session) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	if m.sessions == nil {
		m.sessions = make(map[string]*session.Session)
	}
	m.sessions[s.Token] = s
	return "", nil
}

func (m *memRepo) ForDevice(_ context.Context, _ uuid.UUID) (*session.Session, error) {
	return nil, session.ErrSessionNotFound
}

func (m *memRepo) MarkOutcome(_ context.Context, _ string, _ int) (bool, error) {
	return true, nil
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

func (m *memRepo) DeleteRelaySession(_ context.Context, _ string) error {
	return m.relayDeleteErr
}

func (m *memRepo) ListActiveForDevice(_ context.Context, _ uuid.UUID) ([]*session.Session, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return nil, nil
}

func (m *memRepo) DeleteStale(_ context.Context, cutoff time.Time, keep []string) (int, error) {
	m.staleCalls = append(m.staleCalls, staleCall{cutoff: cutoff, keep: keep})
	if m.staleErr != nil {
		return 0, m.staleErr
	}
	return m.staleDeleted, nil
}

func TestInstrumented_ObservesEveryOperation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name    string
		inner   *memRepo
		call    func(r *session.Instrumented) error
		op      string
		wantErr error
	}{
		{"create", &memRepo{}, func(r *session.Instrumented) error {
			_, err := r.Create(ctx, &session.Session{Token: "t1"})
			return err
		}, "session.Create", nil},
		{"get failing", &memRepo{getErr: sql.ErrConnDone}, func(r *session.Instrumented) error {
			_, err := r.Get(ctx, "tok")
			return err
		}, "session.Get", sql.ErrConnDone},
		{"list a device's sessions", &memRepo{}, func(r *session.Instrumented) error {
			_, err := r.ListActiveForDevice(ctx, uuid.New())
			return err
		}, "session.ListActiveForDevice", nil},
		{"a free device's session", &memRepo{}, func(r *session.Instrumented) error {
			_, err := r.ForDevice(ctx, uuid.New())
			return err
		}, "session.ForDevice", session.ErrSessionNotFound},
		{"mark an outcome", &memRepo{}, func(r *session.Instrumented) error {
			_, err := r.MarkOutcome(ctx, "tok", 0)
			return err
		}, "session.MarkOutcome", nil},
		{"delete", &memRepo{}, func(r *session.Instrumented) error {
			return r.Delete(ctx, "tok")
		}, "session.Delete", nil},
		{"delete a relay session failing", &memRepo{relayDeleteErr: sql.ErrConnDone}, func(r *session.Instrumented) error {
			return r.DeleteRelaySession(ctx, "tok")
		}, "session.DeleteRelaySession", sql.ErrConnDone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			obs := &fakeObserver{}
			err := tc.call(session.NewInstrumented(tc.inner, obs))
			assert.ErrorIs(t, err, tc.wantErr)
			require.Len(t, obs.calls, 1)
			assert.Equal(t, tc.op, obs.calls[0].op)
			assert.Equal(t, tc.wantErr == nil, obs.calls[0].ok)
		})
	}
}
