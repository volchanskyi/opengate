package relay_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// These tests cover the Redis-specific guarantees the in-process adapter does
// not provide: TTL-based reclaim of a dead owner's affinity, and atomic
// convergence when many servers race to claim the same token.

// TTL is honored (unlike InProcess): once the affinity key expires, a different
// server reclaims ownership.
func TestRedisRegistry_TTLExpiryAllowsReclaim(t *testing.T) {
	t.Parallel()
	r, mr := newRedisRegistry(t)
	tok := protocol.SessionToken("tok-ttl")

	owner, err := r.ClaimAffinity(context.Background(), tok, "srv-a", 1*time.Second)
	require.NoError(t, err)
	require.Equal(t, "srv-a", owner)

	mr.FastForward(2 * time.Second) // expire the affinity key

	owner, err = r.ClaimAffinity(context.Background(), tok, "srv-b", 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, "srv-b", owner, "after TTL expiry a new server reclaims")
}

// Many servers racing to claim the same token must converge on exactly one
// owner — the property the atomic Lua claim guarantees. Every caller sees that
// same winner.
func TestRedisRegistry_ConcurrentClaimsConvergeOnOneWinner(t *testing.T) {
	t.Parallel()
	r, _ := newRedisRegistry(t)
	tok := protocol.SessionToken("tok-race")

	const claimers = 16
	owners := make([]string, claimers)
	var wg sync.WaitGroup
	wg.Add(claimers)
	for i := range claimers {
		go func(i int) {
			defer wg.Done()
			owner, err := r.ClaimAffinity(context.Background(), tok, fmt.Sprintf("srv-%d", i), 30*time.Second)
			require.NoError(t, err)
			owners[i] = owner
		}(i)
	}
	wg.Wait()

	winner := owners[0]
	require.NotEmpty(t, winner)
	for _, got := range owners {
		require.Equal(t, winner, got, "all concurrent claimers must see the same winning owner")
	}
	// LookupOwner agrees with the winner the claimers converged on.
	got, err := r.LookupOwner(context.Background(), tok)
	require.NoError(t, err)
	require.Equal(t, winner, got)
}
