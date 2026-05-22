package notifications_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestPostgres_WebPushCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewTestStore(t)
	owner := testutil.SeedUser(t, ctx, store)
	repo := testutil.NewTestWebPush(t, store)

	t.Run("upsert and list", func(t *testing.T) {
		sub := &notifications.WebPushSubscription{
			Endpoint: "https://push.example.com/" + uuid.New().String()[:8],
			UserID:   owner.ID,
			P256dh:   "key123",
			Auth:     "auth456",
		}
		require.NoError(t, repo.Upsert(ctx, sub))

		subs, err := repo.ListForUser(ctx, owner.ID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(subs), 1)

		found := false
		for _, got := range subs {
			if got.Endpoint == sub.Endpoint {
				assert.Equal(t, "key123", got.P256dh)
				assert.Equal(t, "auth456", got.Auth)
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("upsert updates existing", func(t *testing.T) {
		endpoint := "https://push.example.com/update-" + uuid.New().String()[:8]
		sub := &notifications.WebPushSubscription{Endpoint: endpoint, UserID: owner.ID, P256dh: "old", Auth: "old"}
		require.NoError(t, repo.Upsert(ctx, sub))

		sub.P256dh = "new"
		require.NoError(t, repo.Upsert(ctx, sub))

		subs, err := repo.ListForUser(ctx, owner.ID)
		require.NoError(t, err)
		for _, got := range subs {
			if got.Endpoint == endpoint {
				assert.Equal(t, "new", got.P256dh)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		endpoint := "https://push.example.com/del-" + uuid.New().String()[:8]
		sub := &notifications.WebPushSubscription{Endpoint: endpoint, UserID: owner.ID}
		require.NoError(t, repo.Upsert(ctx, sub))
		require.NoError(t, repo.Delete(ctx, endpoint))

		subs, err := repo.ListForUser(ctx, owner.ID)
		require.NoError(t, err)
		for _, got := range subs {
			assert.NotEqual(t, endpoint, got.Endpoint)
		}
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, "https://push.example.com/nope")
		assert.True(t, errors.Is(err, notifications.ErrSubscriptionNotFound))
	})

	t.Run("list all across users", func(t *testing.T) {
		u1 := testutil.SeedUser(t, ctx, store)
		u2 := testutil.SeedUser(t, ctx, store)
		sub1 := &notifications.WebPushSubscription{Endpoint: "https://push.example.com/u1-" + uuid.New().String()[:8], UserID: u1.ID, P256dh: "k1", Auth: "a1"}
		sub2 := &notifications.WebPushSubscription{Endpoint: "https://push.example.com/u2-" + uuid.New().String()[:8], UserID: u2.ID, P256dh: "k2", Auth: "a2"}
		require.NoError(t, repo.Upsert(ctx, sub1))
		require.NoError(t, repo.Upsert(ctx, sub2))

		all, err := repo.ListAll(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(all), 2)

		endpoints := make(map[string]bool)
		for _, s := range all {
			endpoints[s.Endpoint] = true
		}
		assert.True(t, endpoints[sub1.Endpoint])
		assert.True(t, endpoints[sub2.Endpoint])
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

// memWebPushRepo is an in-memory WebPushRepository used for testing the
// Instrumented decorator without Postgres.
type memWebPushRepo struct {
	upsertErr error
	listErr   error
	subs      []*notifications.WebPushSubscription
}

func (m *memWebPushRepo) Upsert(_ context.Context, s *notifications.WebPushSubscription) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.subs = append(m.subs, s)
	return nil
}

func (m *memWebPushRepo) ListForUser(_ context.Context, _ uuid.UUID) ([]*notifications.WebPushSubscription, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.subs, nil
}

func (m *memWebPushRepo) ListAll(_ context.Context) ([]*notifications.WebPushSubscription, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.subs, nil
}

func (m *memWebPushRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func TestInstrumented_ObservesUpsert(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := notifications.NewInstrumentedWebPush(&memWebPushRepo{}, obs)

	require.NoError(t, repo.Upsert(context.Background(), &notifications.WebPushSubscription{
		Endpoint: "https://example.com/a",
		UserID:   uuid.New(),
	}))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "notifications.WebPush.Upsert", obs.calls[0].op)
	assert.True(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesListAllError(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := notifications.NewInstrumentedWebPush(&memWebPushRepo{listErr: sql.ErrConnDone}, obs)

	_, err := repo.ListAll(context.Background())
	require.Error(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "notifications.WebPush.ListAll", obs.calls[0].op)
	assert.False(t, obs.calls[0].ok)
}

func TestInstrumented_ObservesListForUser(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := notifications.NewInstrumentedWebPush(&memWebPushRepo{}, obs)

	_, err := repo.ListForUser(context.Background(), uuid.New())
	require.NoError(t, err)

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "notifications.WebPush.ListForUser", obs.calls[0].op)
}

func TestInstrumented_ObservesDelete(t *testing.T) {
	t.Parallel()
	obs := &fakeObserver{}
	repo := notifications.NewInstrumentedWebPush(&memWebPushRepo{}, obs)

	require.NoError(t, repo.Delete(context.Background(), "https://example.com/a"))

	require.Len(t, obs.calls, 1)
	assert.Equal(t, "notifications.WebPush.Delete", obs.calls[0].op)
}
