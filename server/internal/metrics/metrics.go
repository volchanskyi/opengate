// Package metrics provides Prometheus instrumentation for the OpenGate server.
// It exposes HTTP, relay, agent, MPS, signaling, and database metrics via a
// custom registry (not the global default).
package metrics

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// GaugeSource provides runtime gauge values from application components.
type GaugeSource struct {
	ActiveSessions      func() int
	ConnectedAgents     func() int
	ConnectedMPSDevices func() int
	SignalingSuccesses   func() int64
	SignalingFailures    func() int64
}

// Metrics holds all Prometheus metric descriptors for the OpenGate server.
type Metrics struct {
	// HTTP
	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec

	// Relay
	RelayActiveSessions prometheus.Gauge

	// Agents
	AgentsConnected prometheus.Gauge

	// MPS
	MPSConnectedDevices prometheus.Gauge

	// Signaling
	SignalingUpgradesTotal *prometheus.CounterVec

	// Database
	DBQueryDuration *prometheus.HistogramVec
	DBQueriesTotal  *prometheus.CounterVec
	DBSizeBytes     prometheus.Gauge
}

// NewMetrics creates and registers all metrics on the given registry.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "opengate",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests.",
		}, []string{"method", "route", "status_code"}),

		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "opengate",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),

		RelayActiveSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "opengate",
			Name:      "relay_active_sessions",
			Help:      "Number of active relay sessions.",
		}),

		AgentsConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "opengate",
			Name:      "agents_connected",
			Help:      "Number of currently connected agents.",
		}),

		MPSConnectedDevices: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "opengate",
			Name:      "mps_connected_devices",
			Help:      "Number of connected MPS (Intel AMT) devices.",
		}),

		SignalingUpgradesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "opengate",
			Name:      "signaling_upgrades_total",
			Help:      "Total number of WebRTC signaling upgrades.",
		}, []string{"result"}),

		DBQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "opengate",
			Name:      "db_query_duration_seconds",
			Help:      "Database query duration in seconds.",
			Buckets:   []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		}, []string{"operation"}),

		DBQueriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "opengate",
			Name:      "db_queries_total",
			Help:      "Total number of database queries.",
		}, []string{"operation", "status"}),

		DBSizeBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "opengate",
			Name:      "db_size_bytes",
			Help:      "SQLite database file size in bytes.",
		}),
	}

	reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.RelayActiveSessions,
		m.AgentsConnected,
		m.MPSConnectedDevices,
		m.SignalingUpgradesTotal,
		m.DBQueryDuration,
		m.DBQueriesTotal,
		m.DBSizeBytes,
	)

	return m
}

// NewRegistry creates a Prometheus registry with Go and process collectors.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

// StartGaugeUpdater periodically updates gauge metrics from the given source.
// It stops when the context is cancelled.
func StartGaugeUpdater(ctx context.Context, m *Metrics, src GaugeSource, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	update := func() {
		m.RelayActiveSessions.Set(float64(src.ActiveSessions()))
		m.AgentsConnected.Set(float64(src.ConnectedAgents()))
		m.MPSConnectedDevices.Set(float64(src.ConnectedMPSDevices()))
	}

	// Signaling counters are monotonically increasing atomics in the Tracker.
	// We track the previous value and increment the Prometheus counter by the delta.
	var prevSuccess, prevFailure int64
	updateSignaling := func() {
		curSuccess := src.SignalingSuccesses()
		curFailure := src.SignalingFailures()
		if delta := curSuccess - prevSuccess; delta > 0 {
			m.SignalingUpgradesTotal.WithLabelValues("success").Add(float64(delta))
		}
		if delta := curFailure - prevFailure; delta > 0 {
			m.SignalingUpgradesTotal.WithLabelValues("failure").Add(float64(delta))
		}
		prevSuccess = curSuccess
		prevFailure = curFailure
	}

	update()
	updateSignaling()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
			updateSignaling()
		}
	}
}

// StartDBSizeUpdater periodically queries the SQLite database size and updates
// the db_size_bytes gauge. It stops when the context is cancelled.
func StartDBSizeUpdater(ctx context.Context, m *Metrics, rawDB *sql.DB, logger *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	update := func() {
		var pageCount, pageSize int64
		row := rawDB.QueryRowContext(ctx, "PRAGMA page_count")
		if err := row.Scan(&pageCount); err != nil {
			logger.Warn("metrics: failed to query page_count", "error", err)
			return
		}
		row = rawDB.QueryRowContext(ctx, "PRAGMA page_size")
		if err := row.Scan(&pageSize); err != nil {
			logger.Warn("metrics: failed to query page_size", "error", err)
			return
		}
		m.DBSizeBytes.Set(float64(pageCount * pageSize))
	}

	update()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}
