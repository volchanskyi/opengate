// Package metrics provides Prometheus instrumentation for the OpenGate server.
// It exposes HTTP, relay, agent, MPS, signaling, and database metrics via a
// custom registry (not the global default).
package metrics

import (
	"context"
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
	SignalingSuccesses  func() int64
	SignalingFailures   func() int64
}

// Metrics holds all Prometheus metric descriptors for the OpenGate server.
type Metrics struct {
	// HTTP
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

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

	// Edge Sentinel raw-log broker
	DeviceLogPullsTotal   *prometheus.CounterVec
	DeviceLogPullDuration *prometheus.HistogramVec

	// Edge Sentinel telemetry ingest path (WS-4) + reconnect-backfill scheduler
	// (WS-15). These drive the WS-15b sustained-soak / default-on dashboard.
	EdgeTelemetryIngestedTotal     *prometheus.CounterVec
	EdgeTelemetryDropsTotal        *prometheus.CounterVec
	EdgeTelemetryClockClampedTotal *prometheus.CounterVec
	EdgeBackfillDecisionsTotal     *prometheus.CounterVec
	EdgeBackfillActiveSlots        prometheus.Gauge
	EdgeBackfillGrantRate          prometheus.Gauge

	// Investigations: alerts that never became a stored row.
	AlertsSuppressedTotal *prometheus.CounterVec

	// Chart read path
	MetricsGridMisalignedTotal prometheus.Counter
}

// namespace prefixes every series this package exposes.
const namespace = "opengate"

// counterVec, histogramVec, and gauge build a namespaced collector, so each
// metric below reads as its name, help text, and labels instead of repeating an
// options literal.
func counterVec(name, help string, labels ...string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      name,
		Help:      help,
	}, labels)
}

func histogramVec(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      name,
		Help:      help,
		Buckets:   buckets,
	}, labels)
}

func counter(name, help string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      name,
		Help:      help,
	})
}

func gauge(name, help string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      name,
		Help:      help,
	})
}

// NewMetrics creates and registers all metrics on the given registry.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HTTPRequestsTotal: counterVec("http_requests_total",
			"Total number of HTTP requests.",
			"method", "route", "status_code"),

		HTTPRequestDuration: histogramVec("http_request_duration_seconds",
			"HTTP request duration in seconds.",
			prometheus.DefBuckets, "method", "route"),

		RelayActiveSessions: gauge("relay_active_sessions",
			"Number of active relay sessions."),

		AgentsConnected: gauge("agents_connected",
			"Number of currently connected agents."),

		MPSConnectedDevices: gauge("mps_connected_devices",
			"Number of connected MPS (Intel AMT) devices."),

		SignalingUpgradesTotal: counterVec("signaling_upgrades_total",
			"Total number of WebRTC signaling upgrades.",
			"result"),

		DBQueryDuration: histogramVec("db_query_duration_seconds",
			"Database query duration in seconds.",
			[]float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1}, "operation"),

		DBQueriesTotal: counterVec("db_queries_total",
			"Total number of database queries.",
			"operation", "status"),

		DBSizeBytes: gauge("db_size_bytes",
			"Database size in bytes (pg_database_size)."),

		DeviceLogPullsTotal: counterVec("device_log_pulls_total",
			"Total on-demand raw-log broker pulls by outcome. The ok series is the audited pull count.",
			"result"),

		DeviceLogPullDuration: histogramVec("device_log_pull_duration_seconds",
			"On-demand raw-log broker pull duration in seconds by outcome.",
			[]float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15}, "result"),

		EdgeTelemetryIngestedTotal: counterVec("edge_telemetry_ingested_total",
			"Total Edge-Sentinel telemetry control messages accepted for ingest, by control type.",
			"type"),

		EdgeTelemetryDropsTotal: counterVec("edge_telemetry_drops_total",
			"Total Edge-Sentinel telemetry messages dropped by server-side bounds, by reason.",
			"reason"),

		EdgeTelemetryClockClampedTotal: counterVec("edge_telemetry_clock_clamped_total",
			"Total agent-stamped telemetry timestamps pulled inside the accepted clock window, by direction (future, past). A clamped message is still persisted, so this is not a drop.",
			"direction"),

		EdgeBackfillDecisionsTotal: counterVec("edge_backfill_decisions_total",
			"Total reconnect-backfill admission decisions, by decision (grant, defer).",
			"decision"),

		EdgeBackfillActiveSlots: gauge("edge_backfill_active_slots",
			"Number of reconnect-backfill drain slots currently granted across all agents."),

		EdgeBackfillGrantRate: gauge("edge_backfill_grant_rate_samples_per_second",
			"Per-slot ingest rate (samples/sec) of the most recent backfill grant."),

		AlertsSuppressedTotal: counterVec("alerts_suppressed_total",
			"Total alerts that did not become a stored row, by reason. There is no path for asking an endpoint again, so a rising organization_ceiling series is detection being refused rather than noise being filtered.",
			"reason"),

		MetricsGridMisalignedTotal: counter("metrics_grid_misalignment_total",
			"Total chart samples the time-series store returned outside the request-derived grid of the query it answered. The read is issued at the grid's own instants, so any non-zero value is a defect."),
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
		m.DeviceLogPullsTotal,
		m.DeviceLogPullDuration,
		m.EdgeTelemetryIngestedTotal,
		m.EdgeTelemetryDropsTotal,
		m.EdgeTelemetryClockClampedTotal,
		m.EdgeBackfillDecisionsTotal,
		m.EdgeBackfillActiveSlots,
		m.EdgeBackfillGrantRate,
		m.AlertsSuppressedTotal,
		m.MetricsGridMisalignedTotal,
	)

	return m
}

// ObserveEdgeTelemetryIngest counts one accepted Edge-Sentinel telemetry
// message for the given control type (e.g. AgentMetricWindow). It is the
// numerator of the soak dashboard's ingest-rate panels.
func (m *Metrics) ObserveEdgeTelemetryIngest(msgType string) {
	m.EdgeTelemetryIngestedTotal.WithLabelValues(msgType).Inc()
}

// ObserveEdgeTelemetryDrop counts n dropped telemetry messages under one reason
// (an admission bound such as payload_too_large or interval_floor, an
// empty-payload reason such as empty_dims, or a persist-path failure such as
// tenant_missing, persist_failed, persist_slots_full). n is above 1 when one
// coalesced batch carrying several messages is discarded, so the drop count
// stays comparable with the ingest count. Backfill never backpressures live
// paths, so a rising drop rate under soak is the signal that a server-side bound
// is binding.
func (m *Metrics) ObserveEdgeTelemetryDrop(reason string, n int) {
	m.EdgeTelemetryDropsTotal.WithLabelValues(reason).Add(float64(n))
}

// ObserveEdgeTelemetryClockClamp counts one agent-stamped telemetry timestamp
// pulled inside the accepted clock window: direction is future for a host clock
// ahead of the server, past for one behind. The message is still persisted —
// clamping corrects the timestamp rather than discarding the sample — so this
// is deliberately its own counter and never a drop reason.
func (m *Metrics) ObserveEdgeTelemetryClockClamp(direction string) {
	m.EdgeTelemetryClockClampedTotal.WithLabelValues(direction).Inc()
}

// ObserveBackfillDecision records one reconnect-backfill admission decision.
// A grant records its per-slot rate; a defer leaves the grant-rate gauge
// unchanged. active is the scheduler's current live-slot count after the
// decision, letting the dashboard chart storm drain-down.
func (m *Metrics) ObserveBackfillDecision(granted bool, rate uint32, active int) {
	if granted {
		m.EdgeBackfillDecisionsTotal.WithLabelValues("grant").Inc()
		m.EdgeBackfillGrantRate.Set(float64(rate))
	} else {
		m.EdgeBackfillDecisionsTotal.WithLabelValues("defer").Inc()
	}
	m.EdgeBackfillActiveSlots.Set(float64(active))
}

// ObserveAlertSuppressed counts one alert that reached the server and did not
// become a stored row: organization_ceiling for a customer's spent hourly
// budget, duplicate for a reconnect replaying one already stored.
//
// The two are not the same event. A duplicate cost nothing — the alert is
// already held. Suppression cost an incident nobody will be able to
// reconstruct, which is why it is also folded into a storm room carrying the
// count rather than left as a number on a dashboard.
func (m *Metrics) ObserveAlertSuppressed(reason string) {
	m.AlertsSuppressedTotal.WithLabelValues(reason).Inc()
}

// ObserveMetricsGridMisalignment counts n chart samples that arrived outside
// the request-derived grid of the range query they answered. The read path
// issues that query at the grid's own instants, so this counter should stay at
// zero; a rising value means the store's evaluation instants and the axis the
// API publishes have diverged, which would shift every charted value into the
// wrong bucket.
func (m *Metrics) ObserveMetricsGridMisalignment(n int) {
	m.MetricsGridMisalignedTotal.Add(float64(n))
}

// ObserveDeviceLogPull records one on-demand raw-log broker pull against the
// pull-count and pull-duration metrics, keyed by outcome (ok, busy, timeout,
// offline, unsupported, error). The ok series is the audited pull count — every
// ok pull writes exactly one device.logs.read audit event.
func (m *Metrics) ObserveDeviceLogPull(result string, duration time.Duration) {
	m.DeviceLogPullsTotal.WithLabelValues(result).Inc()
	m.DeviceLogPullDuration.WithLabelValues(result).Observe(duration.Seconds())
}

// Observe records a single DB-shaped operation against the standard db_query_*
// metric pair. It lets the per-aggregate Instrumented decorators (audit,
// updater, auth, device, notifications, amt, session) reuse the same
// dashboards without importing this package or duplicating label discipline.
func (m *Metrics) Observe(operation string, duration time.Duration, ok bool) {
	status := "ok"
	if !ok {
		status = "error"
	}
	m.DBQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
	m.DBQueriesTotal.WithLabelValues(operation, status).Inc()
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

// DBSizer returns the current on-disk database size in bytes.
// Implementations use Postgres pg_database_size.
type DBSizer interface {
	Size(ctx context.Context) (int64, error)
}

// StartDBSizeUpdater periodically queries the database size via the provided
// sizer and updates the db_size_bytes gauge. It stops when the context is cancelled.
func StartDBSizeUpdater(ctx context.Context, m *Metrics, sizer DBSizer, logger *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	update := func() {
		size, err := sizer.Size(ctx)
		if err != nil {
			logger.Warn("metrics: failed to query database size", "error", err)
			return
		}
		m.DBSizeBytes.Set(float64(size))
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
