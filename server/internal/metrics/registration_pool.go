package metrics

import (
	"context"
	"database/sql"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Registration outcomes. Enrollment is the one moment a fleet is all doing the
// same thing at once — a rollout, a site coming back after an outage, a
// reconnect storm — so how long it takes and how often it fails is the number
// that says whether the server absorbed it.
const (
	// RegistrationOK is a registration whose device row is written and online.
	RegistrationOK = "ok"
	// RegistrationError is a registration the server refused or could not
	// complete: the device row was not written, or could not be brought online.
	RegistrationError = "error"
)

// registrationResults is the closed outcome vocabulary, exported from start-up
// so an install that has never failed a registration publishes a zero rather
// than nothing at all.
var registrationResults = []string{RegistrationOK, RegistrationError}

// RegistrationResults returns the registration outcome vocabulary.
func RegistrationResults() []string {
	return append([]string(nil), registrationResults...)
}

// Database-pool states. All four are published together because no one of them
// answers the question a saturation report asks: a pool with every connection
// checked out is busy, and the same reading against its ceiling is exhausted.
// Occupancy against the ceiling separates the two.
const (
	dbPoolOpen   = "open"
	dbPoolActive = "active"
	dbPoolIdle   = "idle"
	dbPoolMax    = "max"
)

var dbPoolStates = []string{dbPoolOpen, dbPoolActive, dbPoolIdle, dbPoolMax}

// DBPoolStates returns the database-pool state vocabulary.
func DBPoolStates() []string {
	return append([]string(nil), dbPoolStates...)
}

// DBPoolStats is one reading of the connection pool: how many connections
// exist, how many are checked out, how many are parked, the ceiling they are
// measured against, and the pool's running account of callers that had to wait
// for a connection and how long they spent doing it.
//
// Waits are cumulative rather than instantaneous on purpose. The pool keeps a
// running total, not a live queue length, so a gauge of "callers waiting now"
// would be a number nothing measures. The total is the stronger signal anyway:
// any increase at all says a request queued behind the pool, which is the
// finding a load run is looking for.
type DBPoolStats struct {
	Open         int
	Active       int
	Idle         int
	Max          int
	WaitCount    int64
	WaitDuration time.Duration
}

// DBPoolStatter reports the current pool reading. The database layer owns the
// numbers and knows nothing about this package; the adapter below is what
// carries them across, so the dependency runs one way only.
type DBPoolStatter interface {
	PoolStats() DBPoolStats
}

// SQLPoolStatter adapts a database/sql pool's own statistics to a reading.
// Wrap the store's Stats method with it at the composition root.
type SQLPoolStatter func() sql.DBStats

// PoolStats converts one database/sql reading.
func (f SQLPoolStatter) PoolStats() DBPoolStats {
	stats := f()
	return DBPoolStats{
		Open:         stats.OpenConnections,
		Active:       stats.InUse,
		Idle:         stats.Idle,
		Max:          stats.MaxOpenConnections,
		WaitCount:    stats.WaitCount,
		WaitDuration: stats.WaitDuration,
	}
}

// registrationDurationBuckets span a registration that lands in a few
// milliseconds through one that is queued behind a saturated pool. The upper
// buckets exist so a storm shows as a tail rather than as a flat +Inf.
var registrationDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// newRegistrationAndPoolMetrics builds the registration and pool collectors.
// They are assembled here rather than inline in NewMetrics so the instrument,
// its vocabulary and its seeding stay in one file.
func newRegistrationAndPoolMetrics(m *Metrics) []prometheus.Collector {
	m.AgentRegistrationsTotal = counterVec("agent_registrations_total",
		"Total agent registrations the server completed, by outcome. Counted where the device row is written, so this is enrollment as the server saw it rather than as a client's send buffer reported it.",
		"result")

	m.AgentRegistrationDuration = histogramVec("agent_registration_duration_seconds",
		"Time from an accepted AgentRegister frame to the device row being written and online, by outcome.",
		registrationDurationBuckets, "result")

	m.DBPoolConnections = gaugeVec("db_pool_connections",
		"Database connection-pool occupancy by state: open connections, those checked out, those parked idle, and the ceiling they are measured against.",
		"state")

	m.DBPoolWaitsTotal = counter("db_pool_waits_total",
		"Total callers that had to wait for a database connection. Any increase says a request queued behind the pool rather than running.")

	m.DBPoolWaitSecondsTotal = counter("db_pool_wait_seconds_total",
		"Total time callers spent waiting for a database connection.")

	return []prometheus.Collector{
		m.AgentRegistrationsTotal,
		m.AgentRegistrationDuration,
		m.DBPoolConnections,
		m.DBPoolWaitsTotal,
		m.DBPoolWaitSecondsTotal,
	}
}

// seedRegistrationAndPoolMetrics exports every label of both closed
// vocabularies at zero, so an idle server publishes numbers instead of gaps.
func seedRegistrationAndPoolMetrics(m *Metrics) {
	for _, result := range registrationResults {
		m.AgentRegistrationsTotal.WithLabelValues(result)
	}
	for _, state := range dbPoolStates {
		m.DBPoolConnections.WithLabelValues(state)
	}
}

// ObserveAgentRegistration records one completed registration and how long the
// server took over it. This is the measurement a load harness reads: the
// harness's own clock stops at a local write into the QUIC send buffer, which
// happens long before the device row exists.
func (m *Metrics) ObserveAgentRegistration(result string, duration time.Duration) {
	m.AgentRegistrationsTotal.WithLabelValues(result).Inc()
	m.AgentRegistrationDuration.WithLabelValues(result).Observe(duration.Seconds())
}

// SetDBPool publishes one pool reading across all four state series.
func (m *Metrics) SetDBPool(stats DBPoolStats) {
	m.DBPoolConnections.WithLabelValues(dbPoolOpen).Set(float64(stats.Open))
	m.DBPoolConnections.WithLabelValues(dbPoolActive).Set(float64(stats.Active))
	m.DBPoolConnections.WithLabelValues(dbPoolIdle).Set(float64(stats.Idle))
	m.DBPoolConnections.WithLabelValues(dbPoolMax).Set(float64(stats.Max))
}

// StartDBPoolUpdater publishes a pool reading every interval until ctx is
// cancelled, starting with one read before the first tick so a scrape taken
// right after boot sees the pool. A nil statter leaves the seeded zeros in
// place, which is the honest reading for a build with no pooled database.
func StartDBPoolUpdater(ctx context.Context, m *Metrics, statter DBPoolStatter, interval time.Duration) {
	if statter == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// The pool reports waits as running totals while Prometheus counters are
	// advanced by increment, so each reading contributes only what is new since
	// the last one — the same shape the signaling counters use.
	var prevWaits int64
	var prevWaitSeconds float64
	publish := func() {
		stats := statter.PoolStats()
		m.SetDBPool(stats)
		if delta := stats.WaitCount - prevWaits; delta > 0 {
			m.DBPoolWaitsTotal.Add(float64(delta))
		}
		prevWaits = stats.WaitCount
		waitSeconds := stats.WaitDuration.Seconds()
		if delta := waitSeconds - prevWaitSeconds; delta > 0 {
			m.DBPoolWaitSecondsTotal.Add(delta)
		}
		prevWaitSeconds = waitSeconds
	}

	publish()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}
