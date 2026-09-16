package metrics

import (
	"errors"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// The three counts of what the process is holding right now — machines
// connected, relay sessions open, AMT devices attached — are worked out when the
// page is read.
//
// Each is a single value the process already has: one atomic load apiece, no
// lock and no query. So reading it where it is asked for costs less than the
// timer and goroutine that would copy it, and the number on the page is the
// number now.
//
// The alternative is a copy a timer refreshes, and the cost of that is not
// theoretical. A load run takes its own count of the fleet and the server's in
// one read of this page, and read a shortfall proportional to how fast machines
// were arriving: eight machines at 1.7 arrivals a second, sixty-four at 13.3,
// two hundred at 35.2. Divide each by its arrival rate and the answer is a time
// inside the refresh interval, every time. In the same read, the goroutine count
// — which Go works out when asked — agreed with the run to within a machine. A
// phase holding five hundred was refused for a server "holding" four hundred and
// fifty-eight.
//
// It is the boundary ADR-076 draws, on the side ADR-076 did not have to name.
// That decision put the alert-pack gauges on a timer for two reasons, and both
// are reasons about a query: a scrape must not run one, and one that fails must
// not read as a fleet gone quiet. Neither reaches a number already sitting in
// memory. The database-backed gauges, and the pool statistics that take the
// pool's own lock, stay where they are.

// errIncompleteGaugeSource is a binding that would publish some of the three and
// leave the rest absent, which reads as a series nobody exports rather than as a
// hole in the wiring.
var errIncompleteGaugeSource = errors.New(
	"a runtime-count source needs all three callbacks: active sessions, connected agents, connected MPS devices")

// runtimeCounts publishes the three counts, asking their source at the moment
// the page is gathered.
//
// It publishes nothing at all until something is bound. A count nobody can take
// is not a count of nought: a zero here would say the fleet is empty, which is a
// reading, and one nobody took.
type runtimeCounts struct {
	activeSessions      *prometheus.Desc
	connectedAgents     *prometheus.Desc
	connectedMPSDevices *prometheus.Desc

	source atomic.Pointer[GaugeSource]
}

// newRuntimeCounts builds the collector with the series it publishes.
func newRuntimeCounts() *runtimeCounts {
	return &runtimeCounts{
		activeSessions: desc("relay_active_sessions",
			"Number of active relay sessions."),
		connectedAgents: desc("agents_connected",
			"Number of currently connected agents."),
		connectedMPSDevices: desc("mps_connected_devices",
			"Number of connected MPS (Intel AMT) devices."),
	}
}

// bind points the three counts at the assembled product's own tallies. Binding
// again replaces the source rather than adding a second one, so a process
// assembled twice still answers with one number per series.
func (c *runtimeCounts) bind(src GaugeSource) error {
	if src.ActiveSessions == nil || src.ConnectedAgents == nil || src.ConnectedMPSDevices == nil {
		return errIncompleteGaugeSource
	}
	c.source.Store(&src)
	return nil
}

// Describe sends the three descriptors, which is what makes a duplicate
// registration of the same series a refusal rather than a page carrying it
// twice.
func (c *runtimeCounts) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.activeSessions
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
		c.connectedAgents, prometheus.GaugeValue, float64(src.ConnectedAgents()))
	ch <- prometheus.MustNewConstMetric(
		c.connectedMPSDevices, prometheus.GaugeValue, float64(src.ConnectedMPSDevices()))
}

// BindRuntimeCounts points the three runtime counts at the assembled product, so
// the page answers with what it is holding at the moment it is read.
//
// It belongs to assembly rather than to the background workers: the counts are
// part of what the product is, and an acceptance test that stands the product up
// without starting a single worker still gets an honest page.
func (m *Metrics) BindRuntimeCounts(src GaugeSource) error {
	return m.runtime.bind(src)
}
