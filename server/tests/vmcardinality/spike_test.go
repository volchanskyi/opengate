// Package vmcardinality counts, against a real VictoriaMetrics, the series one device occupies
// and checks that the reference fleet fits the central budget.
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

// The central active-series budget at the reference fleet size.
const (
	referenceAgents = 500
	seriesBudget    = 50_000
)

// vitalSeriesCap is the most central series one device may occupy, as the ingest path enforces.
const vitalSeriesCap = 24

// metricDims is every dimension of opengate_edge_metric_avg a device writes.
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

// anomalyFamilies are the metric families the health summary reports a rate for, beside the
// node-wide rate.
var anomalyFamilies = []string{"cpu", "mem", "disk", "net", "proc"}

const (
	metricAvgName         = "opengate_edge_metric_avg"
	nodeAnomalyRateName   = "opengate_edge_node_anomaly_rate"
	familyAnomalyRateName = "opengate_edge_family_anomaly_rate"
)

// seriesPerDevice is what one device occupies centrally: one series per metric
// dim, one node-wide anomaly rate, and one rate per family.
func seriesPerDevice() int { return len(metricDims) + 1 + len(anomalyFamilies) }

func TestSeriesModelFitsTheCap(t *testing.T) {
	require.Equal(t, 24, seriesPerDevice(), "the vitals a Linux device emits today")
	require.Equal(t, vitalSeriesCap, seriesPerDevice(),
		"a Linux device now occupies the whole cap, so the next vital re-opens it")

	require.LessOrEqual(t, seriesPerDevice()*referenceAgents, seriesBudget,
		"the reference fleet must fit the central budget")
}

func TestDeviceSeriesAreCappedInVM(t *testing.T) {
	base := testvm.BaseURL(t)
	// A fresh run_id on every series scopes the counts to this test in a shared TSDB.
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

func TestFleetFitsTheBudget(t *testing.T) {
	base := testvm.BaseURL(t)
	runID := "vmcard-" + uuid.NewString()

	// A sample of devices is measured and the fleet total projected from it.
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

// generate returns exposition lines for the vitals contract of each device plus extraDims; the
// line count equals the active-series count.
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

// measure flushes VM and re-counts the matching series until the count reaches want or the
// attempts end, then returns the last reading.
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

// countSeries counts the series matching a selector through /api/v1/series, because instant
// queries evaluate 30s in the past under the default -search.latencyOffset.
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
