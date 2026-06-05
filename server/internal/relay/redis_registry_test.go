package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

// newRedisRegistry returns a RedisRegistry backed by a throwaway in-memory
// miniredis (pure Go — always runs, no Docker, no skips per the
// test-determinism rule). The *miniredis handle lets TTL tests FastForward the
// clock.
func newRedisRegistry(t *testing.T) (relay.SessionRegistry, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return relay.NewRedisRegistry(client), mr
}

// mustClaim claims affinity and fails the test on error.
func mustClaim(t *testing.T, r relay.SessionRegistry, tok protocol.SessionToken, serverID string) {
	t.Helper()
	_, err := r.ClaimAffinity(context.Background(), tok, serverID, 30*time.Second)
	require.NoError(t, err)
}

func TestRedisRegistry_ClaimAffinity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		preOwner  string // if non-empty, a prior claim by this server
		serverID  string
		wantOwner string
	}{
		{"first claim wins", "", "srv-a", "srv-a"},
		{"repeated claim by owner returns self", "srv-a", "srv-a", "srv-a"},
		{"conflicting server returns existing owner", "srv-a", "srv-b", "srv-a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, _ := newRedisRegistry(t)
			tok := protocol.SessionToken("tok")
			if tc.preOwner != "" {
				mustClaim(t, r, tok, tc.preOwner)
			}
			owner, err := r.ClaimAffinity(context.Background(), tok, tc.serverID, 30*time.Second)
			require.NoError(t, err)
			require.Equal(t, tc.wantOwner, owner)
		})
	}
}

func TestRedisRegistry_LookupOwner(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		claimBy   string // if non-empty, claim the token first
		wantOwner string
		wantErr   error
	}{
		{"unknown token is not found", "", "", relay.ErrRegistryNotFound},
		{"returns owner after claim", testServerID, testServerID, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, _ := newRedisRegistry(t)
			tok := protocol.SessionToken("tok")
			if tc.claimBy != "" {
				mustClaim(t, r, tok, tc.claimBy)
			}
			owner, err := r.LookupOwner(context.Background(), tok)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantOwner, owner)
		})
	}
}

func TestRedisRegistry_SaveAndDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, _ := newRedisRegistry(t)
	tok := protocol.SessionToken("tok-save")
	meta := relay.SessionMeta{
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
		ExpectedSides: []relay.Side{relay.SideAgent, relay.SideBrowser},
		ServerID:      testServerID,
	}

	require.NoError(t, r.SaveSession(ctx, tok, meta))
	require.NoError(t, r.SaveSession(ctx, tok, meta)) // idempotent

	owner, err := r.LookupOwner(ctx, tok) // an entry exists after Save
	require.NoError(t, err)
	require.Equal(t, testServerID, owner)

	require.NoError(t, r.DeleteSession(ctx, tok))
	_, err = r.LookupOwner(ctx, tok)
	require.ErrorIs(t, err, relay.ErrRegistryNotFound)
	require.NoError(t, r.DeleteSession(ctx, tok)) // no-op
}

// Operations on one token must not disturb another — keys are namespaced per
// token.
func TestRedisRegistry_TokensAreIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, _ := newRedisRegistry(t)
	tokA := protocol.SessionToken("tok-a")
	tokB := protocol.SessionToken("tok-b")

	mustClaim(t, r, tokA, "srv-a")
	mustClaim(t, r, tokB, "srv-b")

	require.NoError(t, r.DeleteSession(ctx, tokA))

	// tokA gone, tokB untouched.
	_, err := r.LookupOwner(ctx, tokA)
	require.ErrorIs(t, err, relay.ErrRegistryNotFound)
	owner, err := r.LookupOwner(ctx, tokB)
	require.NoError(t, err)
	require.Equal(t, "srv-b", owner)
}

func TestRedisRegistry_EmptyServerIDRejected(t *testing.T) {
	t.Parallel()
	r, _ := newRedisRegistry(t)
	_, err := r.ClaimAffinity(context.Background(), protocol.SessionToken("tok-empty"), "", 30*time.Second)
	require.ErrorIs(t, err, relay.ErrInvalidArgument)
}

// When Redis is unreachable, every operation surfaces the transport error
// rather than silently succeeding. Points at a closed port so the failure is
// deterministic.
func TestRedisRegistry_OperationsErrorWhenRedisUnavailable(t *testing.T) {
	t.Parallel()
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listening → connection refused
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	r := relay.NewRedisRegistry(client)
	ctx := context.Background()
	tok := protocol.SessionToken("tok-unavail")

	_, claimErr := r.ClaimAffinity(ctx, tok, "srv", 30*time.Second)
	require.Error(t, claimErr)
	require.Error(t, r.SaveSession(ctx, tok, relay.SessionMeta{ServerID: "srv"}))
	require.Error(t, r.DeleteSession(ctx, tok))
	require.Error(t, r.PublishEvent(ctx, relay.SessionEvent{Kind: relay.EventCreated}))
	_, subErr := r.SubscribeEvents(ctx)
	require.Error(t, subErr)

	// A transport failure must surface as an error, and must not be mistaken
	// for a clean not-found.
	_, lookupErr := r.LookupOwner(ctx, tok)
	require.Error(t, lookupErr)
	require.NotErrorIs(t, lookupErr, relay.ErrRegistryNotFound, "transport error must not look like not-found")
}

// TestRedisRegistry_Ping reports health from the underlying Redis: nil while the
// server is up, an error once it is gone (readiness drains the pod on that).
func TestRedisRegistry_Ping(t *testing.T) {
	t.Parallel()
	r, mr := newRedisRegistry(t)
	require.NoError(t, r.Ping(context.Background()))

	mr.Close() // simulate Redis loss
	require.Error(t, r.Ping(context.Background()))
}
