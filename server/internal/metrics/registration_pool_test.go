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

func TestObserveAgentRegistrationRecordsOutcomeAndDuration(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	m.ObserveAgentRegistration(RegistrationOK, 12*time.Millisecond)
	m.ObserveAgentRegistration(RegistrationOK, 30*time.Millisecond)
	m.ObserveAgentRegistration(RegistrationError, 900*time.Millisecond)

	require.InDelta(t, 2, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(RegistrationOK)), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(RegistrationError)), 0)
	require.Equal(t, 2, testutil.CollectAndCount(m.AgentRegistrationDuration))
}

func TestAgentRegistrationOutcomesAreExportedFromTheStart(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	for _, result := range RegistrationResults() {
		require.InDelta(t, 0, testutil.ToFloat64(m.AgentRegistrationsTotal.WithLabelValues(result)), 0,
			"registration outcome %q must be exported before it first happens", result)
	}
	require.Equal(t, len(RegistrationResults()), testutil.CollectAndCount(m.AgentRegistrationsTotal))
}

func TestDBPoolStatesCoverTheWholePool(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	m.SetDBPool(DBPoolStats{Open: 9, Active: 4, Idle: 5, Max: 25})

	require.InDelta(t, 9, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("open")), 0)
	require.InDelta(t, 4, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("active")), 0)
	require.InDelta(t, 5, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("idle")), 0)
	require.InDelta(t, 25, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("max")), 0)
	require.Equal(t, len(DBPoolStates()), testutil.CollectAndCount(m.DBPoolConnections))
}

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

func TestDBPoolGaugesAreExportedBeforeTheFirstRead(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	for _, state := range DBPoolStates() {
		require.InDelta(t, 0, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues(state)), 0)
	}
}

type poolStatterFunc func() DBPoolStats

func (f poolStatterFunc) PoolStats() DBPoolStats { return f() }

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

func TestStartDBPoolUpdaterToleratesAnUnwiredSource(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NotPanics(t, func() {
		StartDBPoolUpdater(ctx, m, nil, time.Hour)
	})
	require.InDelta(t, 0, testutil.ToFloat64(m.DBPoolConnections.WithLabelValues("open")), 0)
}

func TestRegistrationDurationBucketsAreReadableByWhatReadsTheSeries(t *testing.T) {
	buckets := RegistrationDurationBuckets()

	require.NotEmpty(t, buckets)
	require.Equal(t, registrationDurationBuckets, buckets)
	for i := 1; i < len(buckets); i++ {
		require.Greater(t, buckets[i], buckets[i-1], "buckets are in ascending order")
	}

	buckets[0] = -1
	require.NotEqual(t, -1.0, registrationDurationBuckets[0], "the caller gets a copy, not the series' own bounds")
}

func TestRegistrationDurationReachesAMinute(t *testing.T) {
	buckets := RegistrationDurationBuckets()

	require.Contains(t, buckets, 30.0)
	require.Contains(t, buckets, 60.0)
	require.Equal(t, 60.0, buckets[len(buckets)-1], "a minute is the widest arrival the server describes")
}
