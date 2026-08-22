package db_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// A load run that saturates the database has to be able to say so. Without a
// pool reading, a slow run looks the same whether requests are executing or
// queued behind a connection, and the only evidence is latency — which is the
// symptom, not the constraint.

// TestPoolStatsReportsTheLiveConnectionPool proves the reading describes the
// pool this store actually opened, ceiling included.
func TestPoolStatsReportsTheLiveConnectionPool(t *testing.T) {
	store := testutil.NewTestStore(t)

	stats := metrics.SQLPoolStatter(store.PoolStats).PoolStats()

	assert.Positive(t, stats.Max, "the pool ceiling must be reported, or occupancy has nothing to be measured against")
	assert.GreaterOrEqual(t, stats.Open, stats.Active, "checked-out connections are a subset of open ones")
	assert.Equal(t, stats.Open, stats.Active+stats.Idle, "every open connection is either checked out or parked")
	assert.LessOrEqual(t, stats.Open, stats.Max, "the pool cannot hold more connections than its ceiling")
}

// TestPoolStatsSeesConnectionsInUse proves the active count moves when work is
// in flight, which is what makes it a saturation signal rather than a constant.
func TestPoolStatsSeesConnectionsInUse(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

	tx, err := store.DB().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	assert.GreaterOrEqual(t, store.PoolStats().InUse, 1, "an open transaction holds a connection out of the pool")
}

// TestPoolStatsCountsWaitsWhenThePoolIsTheConstraint proves the wait account
// rises when callers queue. It is the one reading that separates a pool that is
// merely busy from one that is the bottleneck.
func TestPoolStatsCountsWaitsWhenThePoolIsTheConstraint(t *testing.T) {
	store := testutil.NewTestStoreWithPool(t, 1)
	ctx := context.Background()

	before := store.PoolStats().WaitCount

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var one int
			assert.NoError(t, store.DB().QueryRowContext(ctx, "SELECT 1").Scan(&one))
		}()
	}
	wg.Wait()

	after := store.PoolStats()
	assert.Greater(t, after.WaitCount, before, "eight callers against a one-connection pool must queue")
	assert.Positive(t, after.WaitDuration, "a queued caller spent time waiting")
}

// TestPoolStatsSatisfiesTheMetricsStatter keeps the seam honest: the metrics
// package declares what it needs and adapts the pool's own statistics, so the
// dependency runs one way and the database layer never imports metrics.
func TestPoolStatsSatisfiesTheMetricsStatter(t *testing.T) {
	store := testutil.NewTestStore(t)

	var statter metrics.DBPoolStatter = metrics.SQLPoolStatter(store.PoolStats)
	reading := statter.PoolStats()
	assert.Positive(t, reading.Max)
	assert.Equal(t, store.PoolStats().MaxOpenConnections, reading.Max)
}
