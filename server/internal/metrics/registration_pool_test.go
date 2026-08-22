package metrics

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestObserveAgentRegistrationRecordsOutcomeAndDuration proves registration is
// measured where it completes. A load harness that stops its own clock after
// writing the register frame times a local send buffer, so the number it
// reports is structurally near zero whatever the server does; the outcome and
// duration here are the server's own account of the same event.
func TestObserveAgentRegistrationRecordsOutcomeAndDuration(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	m.ObserveAgentRegistration(RegistrationOK, 12*time.Millisecond)
	m.ObserveAgentRegistration(RegistrationOK, 30*time.Millisecond)
	m.ObserveAgentRegistration(RegistrationError, 900*time.Millisecond)

	require.InDelta(t, 2, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(RegistrationOK)), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(RegistrationError)), 0)
	require.Equal(t, 2, testutil.CollectAndCount(m.AgentRegistrationDuration))
}

// TestAgentRegistrationOutcomesAreExportedFromTheStart keeps a fleet that has
// never failed distinguishable from a server nobody has asked. A missing series
// reads as "no data", which is not the same answer as "no failures".
func TestAgentRegistrationOutcomesAreExportedFromTheStart(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	for _, result := range RegistrationResults() {
		require.InDelta(t, 0, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(result)), 0,
			"registration outcome %q must be exported before it first happens", result)
	}
	require.Equal(t, len(RegistrationResults()), testutil.CollectAndCount(m.AgentRegistrationsTotal))
}

// TestDBPoolStatesCoverTheWholePool proves the four pool states are exported
// together. Reading only the in-use count cannot tell a pool that is busy from
// one that is exhausted; the ceiling beside it is what separates them.
func TestDBPoolStatesCoverTheWholePool(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	m.SetDBPool(DBPoolStats{Open: 9, Active: 4, Idle: 5, Max: 25})

	require.InDelta(t, 9, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("open")), 0)
	require.InDelta(t, 4, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("active")), 0)
	require.InDelta(t, 5, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("idle")), 0)
	require.InDelta(t, 25, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("max")), 0)
	require.Equal(t, len(DBPoolStates()), testutil.CollectAndCount(m.DBPoolConnections))
}

// TestDBPoolWaitsAdvanceByDelta proves a queued caller reaches the counter
// exactly once. The pool keeps a running total; the counter is advanced by
// increment, so re-reading the same total must add nothing.
func TestDBPoolWaitsAdvanceByDelta(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var stats atomic.Pointer[DBPoolStats]
	stats.Store(&DBPoolStats{Open: 25, Active: 25, Max: 25, WaitCount: 4, WaitDuration: 200 * time.Millisecond})

	done := make(chan struct{})
	go func() {
		StartDBPoolUpdater(ctx, m, poolStatterFunc(func() DBPoolStats { return *stats.Load() }), time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.DBPoolWaitsTotal) == 4 &&
			testutil.ToFloat64(m.DBPoolWaitSecondsTotal) == 0.2
	}, time.Second, 5*time.Millisecond, "first reading contributes the whole running total")

	stats.Store(&DBPoolStats{Open: 25, Active: 25, Max: 25, WaitCount: 9, WaitDuration: 500 * time.Millisecond})
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.DBPoolWaitsTotal) == 9 &&
			testutil.ToFloat64(m.DBPoolWaitSecondsTotal) == 0.5
	}, time.Second, 5*time.Millisecond, "a later reading contributes only what is new")

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
}

// TestDBPoolWaitsIgnoreAPoolThatRestarts — a pool rebuilt behind the updater
// reports a total lower than the last one. Adding that as a negative delta
// would make a counter go backwards, which no counter may do.
func TestDBPoolWaitsIgnoreAPoolThatRestarts(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var stats atomic.Pointer[DBPoolStats]
	stats.Store(&DBPoolStats{WaitCount: 7, WaitDuration: time.Second})

	go StartDBPoolUpdater(ctx, m, poolStatterFunc(func() DBPoolStats { return *stats.Load() }), time.Millisecond)
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.DBPoolWaitsTotal) == 7
	}, time.Second, 5*time.Millisecond)

	stats.Store(&DBPoolStats{WaitCount: 1, WaitDuration: 100 * time.Millisecond})
	require.Never(t, func() bool {
		return testutil.ToFloat64(m.DBPoolWaitsTotal) != 7 ||
			testutil.ToFloat64(m.DBPoolWaitSecondsTotal) != 1
	}, 100*time.Millisecond, 10*time.Millisecond)
}

// TestDBPoolGaugesAreExportedBeforeTheFirstRead — same reason as the
// registration outcomes: an idle server must publish zeros, not silence.
func TestDBPoolGaugesAreExportedBeforeTheFirstRead(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	for _, state := range DBPoolStates() {
		require.InDelta(t, 0, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues(state)), 0)
	}
}

type poolStatterFunc func() DBPoolStats

func (f poolStatterFunc) PoolStats() DBPoolStats { return f() }

// TestStartDBPoolUpdaterReadsOnceBeforeItsFirstTick means a scrape taken
// straight after boot sees the pool rather than a zero that reads as an idle
// system.
func TestStartDBPoolUpdaterReadsOnceBeforeItsFirstTick(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	StartDBPoolUpdater(ctx, m, poolStatterFunc(func() DBPoolStats {
		return DBPoolStats{Open: 3, Active: 1, Idle: 2, Max: 25}
	}), time.Hour)

	require.InDelta(t, 3, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("open")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("active")), 0)
}

// TestStartDBPoolUpdaterStopsOnCancel keeps the ticker goroutine from outliving
// the server it observes.
func TestStartDBPoolUpdaterStopsOnCancel(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		StartDBPoolUpdater(ctx, m, poolStatterFunc(func() DBPoolStats {
			return DBPoolStats{}
		}), time.Millisecond)
		close(done)
	}()

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
}

// TestStartDBPoolUpdaterToleratesAnUnwiredSource — a build without a pooled
// database still runs the loop, and must not panic when nothing reports.
func TestStartDBPoolUpdaterToleratesAnUnwiredSource(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NotPanics(t, func() {
		StartDBPoolUpdater(ctx, m, nil, time.Hour)
	})
	require.InDelta(t, 0, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("open")), 0)
}
