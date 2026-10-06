// Package metrics provides Prometheus instrumentation for the OpenGate server
// through a custom registry.
package metrics

import (
	"context"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// GaugeSource supplies the runtime counts; each callback is one read of a tally the process keeps.
type GaugeSource struct {
	ActiveSessions      func() int
	SessionsStarted     func() uint64
	ConnectedAgents     func() int
	ConnectedMPSDevices func() int
}

// Metrics holds all Prometheus metric descriptors for the OpenGate server.
type Metrics struct {
	// HTTP
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

	// Live counts are read when the page is built, so the page shows the value at scrape time.
	runtime *runtimeCounts

	// Agent registration, measured server-side where the device row lands.
	AgentRegistrationsTotal   *prometheus.CounterVec
	AgentRegistrationDuration *prometheus.HistogramVec

	// The resumed split comes from the server's connection state, since a client
	// can present a ticket the server declines.
	AgentTLSHandshakesTotal *prometheus.CounterVec

	// Every audited action is written, failed or shed, so the three outcomes account for each row.
	AuditWritesTotal *prometheus.CounterVec

	// Database
	DBQueryDuration *prometheus.HistogramVec
	DBQueriesTotal  *prometheus.CounterVec
	DBSizeBytes     prometheus.Gauge
	// Pool occupancy plus cumulative callers that queued for a connection.
	DBPoolConnections      *prometheus.GaugeVec
	DBPoolWaitsTotal       prometheus.Counter
	DBPoolWaitSecondsTotal prometheus.Counter

	// Edge Sentinel raw-log broker
	DeviceLogPullsTotal   *prometheus.CounterVec
	DeviceLogPullDuration *prometheus.HistogramVec

	// Edge Sentinel telemetry ingest path and reconnect-backfill scheduler.
	EdgeTelemetryIngestedTotal     *prometheus.CounterVec
	EdgeTelemetryDropsTotal        *prometheus.CounterVec
	EdgeTelemetryClockClampedTotal *prometheus.CounterVec
	EdgeBackfillDecisionsTotal     *prometheus.CounterVec
	EdgeBackfillActiveSlots        prometheus.Gauge
	EdgeBackfillGrantRate          prometheus.Gauge

	// Investigations meta-monitoring of the rule pack; every series here is O(rules).
	AlertsSuppressedTotal *prometheus.CounterVec
	AlertsCreatedTotal    *prometheus.CounterVec
	AlertsOpen            prometheus.Gauge
	IncidentsOpen         *prometheus.GaugeVec
	RuleCoverage          *prometheus.GaugeVec

	// Chart read path
	MetricsGridMisalignedTotal prometheus.Counter

	// rules is the rule-id vocabulary bounding the investigation series, set once at start-up.
	rules atomic.Pointer[ruleVocabulary]
}

// namespace prefixes every series this package exposes.
const namespace = "opengate"

// counterVec builds a namespaced counter vector.
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

// desc builds a namespaced descriptor for a collector that renders its own metrics.
func desc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc(namespace+"_"+name, help, nil, nil)
}

func gaugeVec(name, help string, labels ...string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      name,
		Help:      help,
	}, labels)
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

		runtime: newRuntimeCounts(),

		AuditWritesTotal: counterVec("audit_writes_total",
			"Audited actions by what became of their row.",
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

		AlertsCreatedTotal: counterVec("alerts_created_total",
			"Total alerts that became a stored row, by the rule that raised them. Replays and refusals are not counted, so increase() over this is new detection and nothing else — which is what makes it the numerator of the alerts-per-device-per-day rate.",
			"rule_id"),

		AlertsOpen: gauge("alerts_open",
			"Alerts currently sitting in an incident that is not resolved. An alert holding no room is sub-threshold detail waiting for something to make it meaningful, and is not on anybody's queue."),

		IncidentsOpen: gaugeVec("incidents_open",
			"Incidents that are not resolved, by where each one stands. new is the triage queue — there is no separate promotion entity.",
			"status"),

		RuleCoverage: gaugeVec("rule_coverage",
			"Machines in each coverage state for each rule, across the whole install. The four states always add up to the fleet, so a rule quietly evaluating on half an estate is visible rather than reading as healthy.",
			"rule_id", "state"),

		AgentTLSHandshakesTotal: counterVec("agent_tls_handshakes_total",
			"Total agent QUIC connections that reached the application handshake, by whether the TLS session resumed. The population is connections whose control stream opened, so one lost before that is not counted. Dividing the resumed series by the sum of both is the share of reconnects that skipped the asymmetric handshake.",
			"resumed"),

		MetricsGridMisalignedTotal: counter("metrics_grid_misalignment_total",
			"Total chart samples the time-series store returned outside the request-derived grid of the query it answered. The read is issued at the grid's own instants, so any non-zero value is a defect."),
	}

	reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.runtime,
		m.AuditWritesTotal,
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
		m.AlertsCreatedTotal,
		m.AlertsOpen,
		m.IncidentsOpen,
		m.RuleCoverage,
		m.AgentTLSHandshakesTotal,
		m.MetricsGridMisalignedTotal,
	)
	reg.MustRegister(newRegistrationAndPoolMetrics(m)...)
	seedRegistrationAndPoolMetrics(m)

	// Every status is exported from start-up so an empty queue reads 0 and never "no data".
	for _, status := range openIncidentStatuses {
		m.IncidentsOpen.WithLabelValues(status)
	}

	// Both outcomes exist from start-up because the resumption share divides by their sum.
	m.AgentTLSHandshakesTotal.WithLabelValues("true")
	m.AgentTLSHandshakesTotal.WithLabelValues("false")

	m.seedOutcomes()

	return m
}

// ObserveAuditWrite counts one audited action as written, failed, or shed
// because every write slot was busy.
func (m *Metrics) ObserveAuditWrite(result string) {
	m.AuditWritesTotal.WithLabelValues(result).Inc()
}

// ObserveEdgeTelemetryIngest counts one accepted telemetry message of the given control type.
func (m *Metrics) ObserveEdgeTelemetryIngest(msgType string) {
	m.EdgeTelemetryIngestedTotal.WithLabelValues(msgType).Inc()
}

// ObserveEdgeTelemetryDrop counts n dropped telemetry messages under one reason.
// n exceeds 1 when a coalesced batch is discarded, keeping drops comparable with ingests.
func (m *Metrics) ObserveEdgeTelemetryDrop(reason string, n int) {
	m.EdgeTelemetryDropsTotal.WithLabelValues(reason).Add(float64(n))
}

// ObserveEdgeTelemetryClockClamp counts one timestamp pulled inside the accepted clock window.
// Direction is future for a host clock ahead of the server; the sample is still persisted.
func (m *Metrics) ObserveEdgeTelemetryClockClamp(direction string) {
	m.EdgeTelemetryClockClampedTotal.WithLabelValues(direction).Inc()
}

// ObserveBackfillDecision records one reconnect-backfill admission decision.
// A grant sets the grant-rate gauge; active is the live-slot count after the decision.
func (m *Metrics) ObserveBackfillDecision(granted bool, rate uint32, active int) {
	if granted {
		m.EdgeBackfillDecisionsTotal.WithLabelValues(backfillGrant).Inc()
		m.EdgeBackfillGrantRate.Set(float64(rate))
	} else {
		m.EdgeBackfillDecisionsTotal.WithLabelValues(backfillDefer).Inc()
	}
	m.EdgeBackfillActiveSlots.Set(float64(active))
}

// ObserveAlertSuppressed counts one alert refused a stored row.
// organization_ceiling is a spent hourly budget.
func (m *Metrics) ObserveAlertSuppressed(reason string) {
	m.AlertsSuppressedTotal.WithLabelValues(reason).Inc()
}

// ObserveAgentTLSHandshake counts one agent connection by whether its TLS session resumed.
// It is called once per connection, server side, where the outcome is known.
func (m *Metrics) ObserveAgentTLSHandshake(resumed bool) {
	m.AgentTLSHandshakesTotal.WithLabelValues(strconv.FormatBool(resumed)).Inc()
}

// ObserveMetricsGridMisalignment counts n chart samples outside the request-derived grid.
// The query runs at the grid's own instants, so any non-zero value is a defect.
func (m *Metrics) ObserveMetricsGridMisalignment(n int) {
	m.MetricsGridMisalignedTotal.Add(float64(n))
}

// ObserveDeviceLogPull records one raw-log broker pull by outcome.
// Every ok pull writes exactly one device.logs.read audit event.
func (m *Metrics) ObserveDeviceLogPull(result string, duration time.Duration) {
	m.DeviceLogPullsTotal.WithLabelValues(result).Inc()
	m.DeviceLogPullDuration.WithLabelValues(result).Observe(duration.Seconds())
}

// Observe records one DB-shaped operation against the db_query_* metric pair.
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
