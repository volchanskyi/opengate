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

func TestPoolStatsReportsTheLiveConnectionPool(t *testing.T) {
	store := testutil.NewTestStore(t)

	stats := metrics.SQLPoolStatter(store.PoolStats).PoolStats()

	assert.Positive(t, stats.Max, "the pool ceiling must be reported, or occupancy has nothing to be measured against")
	assert.GreaterOrEqual(t, stats.Open, stats.Active, "checked-out connections are a subset of open ones")
	assert.Equal(t, stats.Open, stats.Active+stats.Idle, "every open connection is either checked out or parked")
	assert.LessOrEqual(t, stats.Open, stats.Max, "the pool cannot hold more connections than its ceiling")
}

func TestPoolStatsSeesConnectionsInUse(t *testing.T) {
	store := testutil.NewTestStore(t)
	ctx := context.Background()

	tx, err := store.DB().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	assert.GreaterOrEqual(t, store.PoolStats().InUse, 1, "an open transaction holds a connection out of the pool")
}

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

func TestPoolStatsSatisfiesTheMetricsStatter(t *testing.T) {
	store := testutil.NewTestStore(t)

	var statter metrics.DBPoolStatter = metrics.SQLPoolStatter(store.PoolStats)
	reading := statter.PoolStats()
	assert.Positive(t, reading.Max)
	assert.Equal(t, store.PoolStats().MaxOpenConnections, reading.Max)
}
