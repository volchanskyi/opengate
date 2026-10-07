package metrics

import (
	"errors"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// errIncompleteGaugeSource rejects a binding that would publish only some of the counts.
var errIncompleteGaugeSource = errors.New(
	"a runtime-count source needs every callback: active sessions, sessions started, connected agents, connected MPS devices")

// runtimeCounts publishes the live counts by asking their source when the page is gathered.
// It publishes nothing until a source is bound, since a zero would claim an empty fleet.
type runtimeCounts struct {
	activeSessions      *prometheus.Desc
	sessionsStarted     *prometheus.Desc
	connectedAgents     *prometheus.Desc
	connectedMPSDevices *prometheus.Desc

	source atomic.Pointer[GaugeSource]
}

// newRuntimeCounts builds the collector with the series it publishes.
func newRuntimeCounts() *runtimeCounts {
	return &runtimeCounts{
		activeSessions: desc("relay_active_sessions",
			"Number of active relay sessions."),
		sessionsStarted: desc("relay_sessions_started_total",
			"Relay sessions opened since the process started, counted once per session."),
		connectedAgents: desc("agents_connected",
			"Number of currently connected agents."),
		connectedMPSDevices: desc("mps_connected_devices",
			"Number of connected MPS (Intel AMT) devices."),
	}
}

// bind points the counts at the product's own tallies; a later bind replaces the source.
func (c *runtimeCounts) bind(src GaugeSource) error {
	if src.ActiveSessions == nil || src.SessionsStarted == nil ||
		src.ConnectedAgents == nil || src.ConnectedMPSDevices == nil {
		return errIncompleteGaugeSource
	}
	c.source.Store(&src)
	return nil
}

// Describe sends the descriptors, so a duplicate registration of the same series is refused.
func (c *runtimeCounts) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.activeSessions
	ch <- c.sessionsStarted
	ch <- c.connectedAgents
	ch <- c.connectedMPSDevices
}

// Collect asks the source for each count as the page is being built.
func (c *runtimeCounts) Collect(ch chan<- prometheus.Metric) {
	src := c.source.Load()
	if src == nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(
		c.activeSessions, prometheus.GaugeValue, float64(src.ActiveSessions()))
	ch <- prometheus.MustNewConstMetric(
		c.sessionsStarted, prometheus.CounterValue, float64(src.SessionsStarted()))
	ch <- prometheus.MustNewConstMetric(
		c.connectedAgents, prometheus.GaugeValue, float64(src.ConnectedAgents()))
	ch <- prometheus.MustNewConstMetric(
		c.connectedMPSDevices, prometheus.GaugeValue, float64(src.ConnectedMPSDevices()))
}

// BindRuntimeCounts points the runtime counts at the assembled product's tallies.
func (m *Metrics) BindRuntimeCounts(src GaugeSource) error {
	return m.runtime.bind(src)
}
