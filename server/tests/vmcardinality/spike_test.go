// Package vmcardinality measures what one device actually costs the central
// store, against a real VictoriaMetrics.
//
// Cardinality — not sample rate — is the binding constraint on the central
// store, so the vitals contract fixes how many series a device may occupy and
// this package proves the number rather than projecting it. Two properties are
// under test:
//
//   - A device occupies at most vitalSeriesCap series, whatever it sends. The
//     count is a compile-time constant of the contract, not a function of how
//     large the host is or of what dim names an agent chooses to invent.
//   - The whole reference fleet fits the budget at that per-device cost, which
//     is what makes central growth linear in agent count.
//
// The dims written here are the contract the agent emits and the server
// allowlists; a name added on either side without being added here writes a
// series this measurement does not account for.
package vmcardinality

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// Central active-series budget at the reference fleet size. A single-node
// VictoriaMetrics handles far more, but this keeps the free-tier volume and
// query latency comfortable; exceeding it is a signal to revisit the schema.
const (
	referenceAgents = 500
	seriesBudget    = 50_000
)

// vitalSeriesCap is the most central series one device may occupy — the same
// cap the ingest path enforces (server/internal/agentapi/vitals.go). A Linux
// device now writes exactly this many.
const vitalSeriesCap = 24

// metricDims is every dimension of opengate_edge_metric_avg a device writes:
// each gauge's average, the window maximum for the five where a within-minute
// spike is the signal, the five stall vitals a Linux device reads from the
// kernel's pressure accounting, and the three that say how slow its disks are.
var metricDims = []string{
	"cpu.total",
	"cpu.total.max",
	"mem.used_percent",
	"mem.used_percent.max",
	"disk.used_percent",
	"net.rx_bps",
	"net.rx_bps.max",
	"net.tx_bps",
	"net.tx_bps.max",
	"disk.mounts_critical",
	"stall.cpu.some",
	"stall.mem.some",
	"stall.mem.full",
	"stall.io.some",
	"stall.io.full",
	"disk.await_ms",
	"disk.await_ms.max",
	"disk.queue_depth",
}

// anomalyFamilies are the metric families the health summary reports a rate for,
// beside the one node-wide rate.
var anomalyFamilies = []string{"cpu", "mem", "disk", "net", "proc"}

const (
	metricAvgName         = "opengate_edge_metric_avg"
	nodeAnomalyRateName   = "opengate_edge_node_anomaly_rate"
	familyAnomalyRateName = "opengate_edge_family_anomaly_rate"
)

// seriesPerDevice is what one device occupies centrally: one series per metric
// dim, one node-wide anomaly rate, and one rate per family.
func seriesPerDevice() int { return len(metricDims) + 1 + len(anomalyFamilies) }

// TestSeriesModelFitsTheCap pins the per-device cost and its headroom before any
// VM is involved, so a dim added to the contract without a decision fails here.
func TestSeriesModelFitsTheCap(t *testing.T) {
	require.Equal(t, 24, seriesPerDevice(), "the vitals a Linux device emits today")
	require.Equal(t, vitalSeriesCap, seriesPerDevice(),
		"a Linux device now occupies the whole cap, so the next vital re-opens it")

	// Central growth is linear in agent count at this per-device cost.
	require.LessOrEqual(t, seriesPerDevice()*referenceAgents, seriesBudget,
		"the reference fleet must fit the central budget")
}

// TestDeviceSeriesAreCappedInVM writes the contract for real devices and counts
// what VictoriaMetrics actually holds. A count is only meaningful per device, so
// every assertion is scoped to one device_id — the TSDB-wide total says nothing
// about whether a single agent can grow without bound.
func TestDeviceSeriesAreCappedInVM(t *testing.T) {
	base := testvm.BaseURL(t)
	// Every series this run writes carries a fresh run_id, so the counts measure
	// exactly what this test ingested and nothing else in a shared TSDB.
	runID := "vmcard-" + uuid.NewString()

	devices := deviceIDs(runID, 3)
	ingest(t, base, generate(runID, devices, nil))

	for _, device := range devices {
		got := measureDeviceSeries(t, base, runID, device, seriesPerDevice())
		require.Equal(t, seriesPerDevice(), got, "device %s writes its whole contract", device)
		require.LessOrEqualf(t, got, vitalSeriesCap,
			"device %s occupies %d series, over the cap of %d", device, got, vitalSeriesCap)
	}
}

// TestAnUnlistedDimWouldBreachTheCap is the measurement that gives the cap its
// teeth: writing dims beyond the contract for one device raises that device's
// series count past the cap, which is exactly what the server's allowlist stops
// happening from an agent's own message. Here the writes bypass the allowlist —
// they go straight to VM — so the breach is observable rather than theoretical.
func TestAnUnlistedDimWouldBreachTheCap(t *testing.T) {
	base := testvm.BaseURL(t)
	runID := "vmcard-" + uuid.NewString()
	device := deviceIDs(runID, 1)[0]

	extra := make([]string, 0, vitalSeriesCap)
	for i := range vitalSeriesCap - seriesPerDevice() + 1 {
		extra = append(extra, fmt.Sprintf("invented.dim.%d", i))
	}
	want := seriesPerDevice() + len(extra)
	ingest(t, base, generate(runID, []string{device}, extra))

	got := measureDeviceSeries(t, base, runID, device, want)
	require.Equal(t, want, got)
	require.Greaterf(t, got, vitalSeriesCap,
		"one device past the contract must exceed the cap, or the cap measures nothing (%d <= %d)",
		got, vitalSeriesCap)
}

// TestFleetFitsTheBudget scales the measured per-device cost to the reference
// fleet and checks the total against the central budget.
func TestFleetFitsTheBudget(t *testing.T) {
	base := testvm.BaseURL(t)
	runID := "vmcard-" + uuid.NewString()

	// Measure a sample of devices, then project: writing 500 devices' worth of
	// series proves nothing the per-device count does not already prove, and it
	// would make the suite pay for 8 000 series to learn it.
	const sample = 25
	devices := deviceIDs(runID, sample)
	ingest(t, base, generate(runID, devices, nil))

	want := sample * seriesPerDevice()
	require.Equal(t, want, measureRunSeries(t, base, runID, want),
		"measured VM active series must match the model for the sample")

	fleet := referenceAgents * seriesPerDevice()
	require.LessOrEqualf(t, fleet, seriesBudget,
		"fleet cardinality (%d) must fit the central budget (%d)", fleet, seriesBudget)
	t.Logf("EVIDENCE %d series/device measured -> %d @%d agents (budget %d, cap %d/device) PASS",
		seriesPerDevice(), fleet, referenceAgents, seriesBudget, vitalSeriesCap)
}

// deviceIDs returns n device ids unique to this run.
func deviceIDs(runID string, n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, fmt.Sprintf("%s-dev-%d", runID, i))
	}
	return ids
}

// generate returns Prometheus exposition lines carrying the vitals contract for
// each device, plus any extraDims — the dims an agent outside the contract would
// write if nothing filtered them. Metric name + label set is unique per (device,
// dimension), so the line count equals the distinct active-series count.
func generate(runID string, devices, extraDims []string) string {
	const tenants = 5
	ts := time.Now().UnixMilli()
	var b strings.Builder
	for i, device := range devices {
		tenant := fmt.Sprintf("tenant-%d", i%tenants)
		emit := func(name, extraKey, extraVal string) {
			if extraKey == "" {
				fmt.Fprintf(&b, "%s{run_id=%q,tenant_id=%q,device_id=%q} 1 %d\n", name, runID, tenant, device, ts)
				return
			}
			fmt.Fprintf(&b, "%s{run_id=%q,tenant_id=%q,device_id=%q,%s=%q} 1 %d\n",
				name, runID, tenant, device, extraKey, extraVal, ts)
		}
		for _, dim := range metricDims {
			emit(metricAvgName, "dim", dim)
		}
		for _, dim := range extraDims {
			emit(metricAvgName, "dim", dim)
		}
		emit(nodeAnomalyRateName, "", "")
		for _, family := range anomalyFamilies {
			emit(familyAnomalyRateName, "family", family)
		}
	}
	return b.String()
}

func ingest(t *testing.T, base, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/api/v1/import/prometheus", strings.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Lessf(t, resp.StatusCode, 300, "import should succeed, got %d", resp.StatusCode)
}

// measureDeviceSeries counts the series one device carries in this run.
func measureDeviceSeries(t *testing.T, base, runID, device string, want int) int {
	t.Helper()
	return measure(t, base, fmt.Sprintf(`{run_id=%q,device_id=%q}`, runID, device), want)
}

// measureRunSeries counts every series this run wrote.
func measureRunSeries(t *testing.T, base, runID string, want int) int {
	t.Helper()
	return measure(t, base, fmt.Sprintf(`{run_id=%q}`, runID), want)
}

// measure flushes VM and re-counts the matching series, retrying (in the same
// goroutine, so -race stays clean) until the count reaches want or the budget of
// attempts is spent — then returns the last reading for the caller to assert on,
// so a mismatch fails loudly rather than skips.
func measure(t *testing.T, base, selector string, want int) int {
	t.Helper()
	var last int
	for range 25 {
		forceFlush(t, base)
		last = countSeries(t, base, selector)
		if last == want {
			return last
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last
}

func forceFlush(t *testing.T, base string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/internal/force_flush", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// countSeries counts the series matching a selector.
//
// /api/v1/series answers this rather than an instant `count()` query: VM's
// default -search.latencyOffset evaluates instant queries 30s in the past, so a
// query would never see samples written moments earlier.
func countSeries(t *testing.T, base, selector string) int {
	t.Helper()
	q := url.Values{"match[]": {selector}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		base+"/api/v1/series?"+q.Encode(), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Data []map[string]string `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return len(result.Data)
}
