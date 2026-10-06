package metrics

import (
	"context"
	"database/sql"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Registration outcomes.
const (
	// RegistrationOK is a registration whose device row is written and online.
	RegistrationOK = "ok"
	// RegistrationError is a registration refused or unable to write or bring online its device row.
	RegistrationError = "error"
)

// registrationResults is the closed outcome vocabulary, exported at zero from start-up.
var registrationResults = []string{RegistrationOK, RegistrationError}

// RegistrationResults returns the registration outcome vocabulary.
func RegistrationResults() []string {
	return append([]string(nil), registrationResults...)
}

// Database-pool states; occupancy against the max ceiling separates busy from exhausted.
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

// DBPoolStats is one reading of the connection pool: open, checked-out and idle connections,
// the ceiling, and the cumulative wait count and duration the pool keeps.
type DBPoolStats struct {
	Open         int
	Active       int
	Idle         int
	Max          int
	WaitCount    int64
	WaitDuration time.Duration
}

// DBPoolStatter reports the current pool reading.
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

// registrationDurationBuckets span a few milliseconds to a minute so a storm shows as a tail.
var registrationDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// RegistrationDurationBuckets returns the registration timing bounds, widest last.
// The widest bound is the slowest registration the server can describe.
func RegistrationDurationBuckets() []float64 {
	return append([]float64(nil), registrationDurationBuckets...)
}

// newRegistrationAndPoolMetrics builds the registration and pool collectors.
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

// seedRegistrationAndPoolMetrics exports every label of both closed vocabularies at zero.
func seedRegistrationAndPoolMetrics(m *Metrics) {
	for _, result := range registrationResults {
		m.AgentRegistrationsTotal.WithLabelValues(result)
	}
	for _, state := range dbPoolStates {
		m.DBPoolConnections.WithLabelValues(state)
	}
}

// ObserveAgentRegistration records one completed registration and the server-side time it took.
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

// StartDBPoolUpdater publishes a pool reading every interval until ctx is cancelled,
// starting with one read. A nil statter leaves the seeded zeros in place.
func StartDBPoolUpdater(ctx context.Context, m *Metrics, statter DBPoolStatter, interval time.Duration) {
	if statter == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// The pool reports running totals, so each reading adds only the increase since the last.
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
