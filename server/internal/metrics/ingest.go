package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// The writer's classes and the outcomes a reading it received settles under.
var (
	ingestClasses  = []string{"live", "catch_up"}
	ingestOutcomes = []string{
		"stored", "invalid", "tombstoned", "queue_full", "write_failed", "waited_too_long", "shutdown",
	}
)

// IngestClasses returns the writer's classes, each published at zero from start-up.
func IngestClasses() []string { return append([]string(nil), ingestClasses...) }

// IngestOutcomes returns the outcomes a received reading settles under.
func IngestOutcomes() []string { return append([]string(nil), ingestOutcomes...) }

// The causes of a request to send history again.
const (
	ResendStoreRestart = "store_restart"
	ResendLiveDrop     = "live_drop"
)

var resendCauses = []string{ResendStoreRestart, ResendLiveDrop}

// ResendCauses returns the causes a request to send again is counted under.
func ResendCauses() []string { return append([]string(nil), resendCauses...) }

// ingestMetrics are the shared numeric writer's readings: per class, never per machine.
type ingestMetrics struct {
	IngestReadingsReceivedTotal *prometheus.CounterVec
	IngestReadingsTotal         *prometheus.CounterVec
	IngestAcceptanceSeconds     *prometheus.HistogramVec
	IngestQueueBytes            *prometheus.GaugeVec
	IngestQueueOldestSeconds    *prometheus.GaugeVec
	IngestRequestsTotal         *prometheus.CounterVec
	IngestRequestSeconds        *prometheus.HistogramVec
	IngestRequestBytes          *prometheus.HistogramVec
	IngestResendRequestsTotal   *prometheus.CounterVec
	IngestStoreRestartsTotal    prometheus.Counter
}

// acceptanceBuckets bracket the 5 s live promise.
var acceptanceBuckets = []float64{0.25, 0.5, 1, 2, 3, 4, 5, 7.5, 10, 15, 30}

func newIngestMetrics() ingestMetrics {
	return ingestMetrics{
		IngestReadingsReceivedTotal: counterVec("ingest_readings_received_total",
			"Readings handed to the numeric writer, by class.", "class"),
		IngestReadingsTotal: counterVec("ingest_readings_total",
			"Readings the numeric writer settled, by class and outcome; the outcomes sum to the readings received.",
			"class", "outcome"),
		IngestAcceptanceSeconds: histogramVec("ingest_acceptance_seconds",
			"Time from a reading reaching the server to the store accepting it, by class; "+
				"readings not stored are counted only in ingest_readings_total.",
			acceptanceBuckets, "class"),
		IngestQueueBytes: gaugeVec("ingest_queue_bytes",
			"Encoded bytes the numeric writer holds, queued and in flight, by class.", "class"),
		IngestQueueOldestSeconds: gaugeVec("ingest_queue_oldest_seconds",
			"Age of the oldest queued hand-off, by class.", "class"),
		IngestRequestsTotal: counterVec("ingest_requests_total",
			"Requests the numeric writer sent the store, by class and result.", "class", "result"),
		IngestRequestSeconds: histogramVec("ingest_request_seconds",
			"Duration of one store request, by class.",
			[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}, "class"),
		IngestRequestBytes: histogramVec("ingest_request_bytes",
			"Encoded size of one store request, by class.",
			prometheus.ExponentialBuckets(4096, 4, 8), "class"),
		IngestResendRequestsTotal: counterVec("ingest_resend_requests_total",
			"Machines asked to send their history again, by cause.", "cause"),
		IngestStoreRestartsTotal: counter("ingest_store_restarts_total",
			"Store restarts the server saw by the store's start time."),
	}
}

func (m ingestMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.IngestReadingsReceivedTotal, m.IngestReadingsTotal, m.IngestAcceptanceSeconds,
		m.IngestQueueBytes, m.IngestQueueOldestSeconds, m.IngestRequestsTotal,
		m.IngestRequestSeconds, m.IngestRequestBytes, m.IngestResendRequestsTotal,
		m.IngestStoreRestartsTotal,
	}
}

func (m ingestMetrics) seed() {
	for _, class := range ingestClasses {
		m.IngestReadingsReceivedTotal.WithLabelValues(class)
		m.IngestQueueBytes.WithLabelValues(class)
		m.IngestQueueOldestSeconds.WithLabelValues(class)
		for _, outcome := range ingestOutcomes {
			m.IngestReadingsTotal.WithLabelValues(class, outcome)
		}
		for _, result := range []string{"ok", "failed"} {
			m.IngestRequestsTotal.WithLabelValues(class, result)
		}
	}
	for _, cause := range resendCauses {
		m.IngestResendRequestsTotal.WithLabelValues(cause)
	}
}

// ReadingsReceived counts readings handed to the writer.
func (m *Metrics) ReadingsReceived(class string, n int) {
	m.IngestReadingsReceivedTotal.WithLabelValues(class).Add(float64(n))
}

// ReadingsSettled counts readings settled under one outcome.
func (m *Metrics) ReadingsSettled(class, outcome string, n int) {
	m.IngestReadingsTotal.WithLabelValues(class, outcome).Add(float64(n))
}

// ReadingsAccepted records the time stored readings took from receipt to the store's answer.
func (m *Metrics) ReadingsAccepted(class string, after time.Duration, n int) {
	observer := m.IngestAcceptanceSeconds.WithLabelValues(class)
	for range n {
		observer.Observe(after.Seconds())
	}
}

// Held publishes what one class holds and how long its oldest hand-off has waited.
func (m *Metrics) Held(class string, bytes int, oldest time.Duration) {
	m.IngestQueueBytes.WithLabelValues(class).Set(float64(bytes))
	m.IngestQueueOldestSeconds.WithLabelValues(class).Set(oldest.Seconds())
}

// Request records one store request's size, duration and result.
func (m *Metrics) Request(class string, bytes int, took time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "failed"
	}
	m.IngestRequestsTotal.WithLabelValues(class, result).Inc()
	m.IngestRequestSeconds.WithLabelValues(class).Observe(took.Seconds())
	m.IngestRequestBytes.WithLabelValues(class).Observe(float64(bytes))
}

// ObserveResendRequests counts machines asked to send again under one cause.
func (m *Metrics) ObserveResendRequests(cause string, machines int) {
	m.IngestResendRequestsTotal.WithLabelValues(cause).Add(float64(machines))
}

// ObserveStoreRestart counts one store restart seen.
func (m *Metrics) ObserveStoreRestart() { m.IngestStoreRestartsTotal.Inc() }
