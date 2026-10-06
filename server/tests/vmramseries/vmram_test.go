// Package vmramseries fits the memory cost of one active series in the central store from a line
// through four or more load points, and measures the disk cost per sample.
package vmramseries

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// The fleet size and the Q2 to Q4 budgets the measurement is compared against, not asserted on.
const (
	// Sizes are decimal, the units the budgets are stated in.
	kilobyte = 1_000
	megabyte = 1_000_000
	gigabyte = 1_000_000_000

	fleetAgents     = 5_000
	seriesBudget    = 120_000        // Q2: active series at fleet scale
	ramBudgetBytes  = 400 * megabyte // Q3: total VictoriaMetrics memory
	diskBudgetBytes = 2 * gigabyte   // Q4: 30 d on disk at fleet scale

	retentionWindow = 30 * 24 * time.Hour
	vitalsCadence   = 60 * time.Second

	// referenceBytesPerSample is the live store's data size over rows added, the cross-check for
	// the harness.
	referenceBytesPerSample = 0.316
)

// vitalDims is every dimension of opengate_edge_metric_avg the sizing shape assumes, including
// the Linux-only vitals.
var vitalDims = []string{
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
	"disk.await_ms",
	"disk.await_ms.max",
	"disk.queue_depth",
	"stall.cpu.some",
	"stall.mem.some",
	"stall.mem.full",
	"stall.io.some",
	"stall.io.full",
}

// anomalyFamilies are the metric families the health summary reports a rate for,
// beside the one node-wide rate.
var anomalyFamilies = []string{"cpu", "mem", "disk", "net", "proc"}

const (
	metricAvgName         = "opengate_edge_metric_avg"
	nodeAnomalyRateName   = "opengate_edge_node_anomaly_rate"
	familyAnomalyRateName = "opengate_edge_family_anomaly_rate"
)

// seriesPerDevice is what one device occupies centrally: one series per vital
// dimension, one node-wide anomaly rate, and one rate per family.
func seriesPerDevice() int { return len(vitalDims) + 1 + len(anomalyFamilies) }

// warmupDevices are written first so every load point sits past the lazy-allocation ramp; their
// reading is excluded from the fit.
const warmupDevices = 1_000

// The load points are total device counts; the environment variable overrides the default scale.
const loadPointsEnv = "OPENGATE_VMRAM_DEVICES"

var defaultLoadPoints = []int{2000, 2600, 3200, 3800, 4400, 5000}

// The disk half uses fewer series, each carrying a run of samples at the cadence.
const (
	diskDevicesEnv     = "OPENGATE_VMRAM_DISK_DEVICES"
	diskMinutesEnv     = "OPENGATE_VMRAM_DISK_MINUTES"
	defaultDiskDevices = 200
	defaultDiskMinutes = 60
)

// vmArgs pin VictoriaMetrics' memory budget so the figure is the same on every host.
var vmArgs = []string{"-memory.allowedBytes=1073741824"}

// VictoriaMetrics refreshes its process metrics once a second, and the Go runtime returns pages
// over several collections, so a load point is read after repeated forced collections.
const (
	rssRefreshInterval = 1200 * time.Millisecond
	rssSettleAttempts  = 12
	rssSettleTolerance = 0.01
)

// gaugeDriftSamples is how many cadence ticks a synthetic gauge takes to swing through a radian.
const gaugeDriftSamples = 30

// settleAttempts and settleInterval bound the wait for written data to reach VictoriaMetrics'
// own accounting.
const (
	settleAttempts = 40
	settleInterval = 250 * time.Millisecond
)

func TestRAMPerActiveSeriesFit(t *testing.T) {
	points, err := parseLoadPoints(os.Getenv(loadPointsEnv))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(points), 4, "a fit needs at least four load points")

	base := testvm.Dedicated(t, vmArgs...)
	version := promLabel(scrape(t, base), "vm_app_version", "short_version")
	require.NotEmpty(t, version, "the build the number belongs to must be recorded with it")
	require.Equal(t, 0, tsdbActiveSeries(t, base),
		"the fit must be taken on a VictoriaMetrics no other test writes to")

	runID := "vmram-" + uuid.NewString()
	devices := deviceIDs(runID, points[len(points)-1])

	ingest(t, base, vitalsExposition(runID, devices[:warmupDevices], 0))
	warmSeries := warmupDevices * seriesPerDevice()
	require.Equal(t, warmSeries, settleSeries(t, base, warmSeries))
	t.Logf("EVIDENCE Q3 warm-up series=%d rss=%.1f MB — the startup ramp, excluded from the fit",
		warmSeries, collectedResidentBytes(t, base)/megabyte)

	readings := make([]seriesRAMPoint, 0, len(points))
	written := warmupDevices
	var topInUse float64
	for i, total := range points {
		ingest(t, base, vitalsExposition(runID, devices[written:total], 0))
		written = total

		want := total * seriesPerDevice()
		got := settleSeries(t, base, want)
		require.Equalf(t, want, got,
			"%d devices must hold %d series; VictoriaMetrics counted %d", total, want, got)

		// The top load point is also read uncollected, a refresh interval after the import.
		if i == len(points)-1 {
			time.Sleep(rssRefreshInterval)
			topInUse = residentBytes(t, base)
		}

		rss := collectedResidentBytes(t, base)
		readings = append(readings, seriesRAMPoint{series: got, rss: rss})
		t.Logf("EVIDENCE Q3 point devices=%d series=%d rss=%.1f MB collected", total, got, rss/megabyte)
	}

	fit, err := fitSeriesRAM(readings)
	require.NoError(t, err)
	require.Greaterf(t, fit.bytesPerSeries, 0.0,
		"memory must grow with series, or the readings measure noise rather than cost (R2=%.4f)", fit.r2)
	require.GreaterOrEqualf(t, fit.r2, 0.5,
		"the line must explain the readings to be a measurement (slope=%.0f B/series, R2=%.4f)",
		fit.bytesPerSeries, fit.r2)
	require.LessOrEqual(t, fit.r2, 1.0)

	last := readings[len(readings)-1]
	t.Logf("EVIDENCE Q3 fit %s: %.0f B/series (%.2f KB), baseline %.1f MB, R2=%.4f, %d points",
		version, fit.bytesPerSeries, fit.bytesPerSeries/kilobyte, fit.baselineBytes/megabyte, fit.r2, len(readings))
	t.Logf("EVIDENCE Q3 single-point division at %d series would answer %.2f KB/series — the baseline this fit separates out",
		last.series, naiveBytesPerSeries(last)/kilobyte)
	t.Logf("EVIDENCE Q3 projection at Q2's %d series: marginal %.1f MB, with baseline %.1f MB (budget %d MB)",
		seriesBudget, fit.marginalRAMBytes(seriesBudget)/megabyte,
		fit.projectRAMBytes(seriesBudget)/megabyte, ramBudgetBytes/megabyte)
	t.Logf("EVIDENCE Q3 the projection is what the data costs: at the top load point the process held %.1f MB with the import's garbage still in it against %.1f MB collected, and a pod pays that difference too",
		topInUse/megabyte, last.rss/megabyte)
}

func TestDiskPerSampleAtVitalsCadence(t *testing.T) {
	devices := envInt(t, diskDevicesEnv, defaultDiskDevices)
	minutes := envInt(t, diskMinutesEnv, defaultDiskMinutes)

	base := testvm.Dedicated(t, vmArgs...)
	version := promLabel(scrape(t, base), "vm_app_version", "short_version")
	require.NotEmpty(t, version)
	require.Equal(t, 0, tsdbActiveSeries(t, base),
		"cost per sample must be measured on a VictoriaMetrics no other test writes to")

	runID := "vmdisk-" + uuid.NewString()
	ids := deviceIDs(runID, devices)
	series := devices * seriesPerDevice()

	// Every write covers the same series set one cadence tick further back.
	for minute := range minutes {
		ingest(t, base, vitalsExposition(runID, ids, minute))
	}

	samples := float64(series * minutes)
	dataBytes := settleDisk(t, base)
	body := scrape(t, base)

	rows, ok := promGauge(body, "vm_rows_added_to_storage_total")
	require.True(t, ok, "VictoriaMetrics must report the rows it stored")
	require.Equalf(t, samples, rows,
		"every written sample must be stored: wrote %.0f, VictoriaMetrics stored %.0f", samples, rows)
	require.Greater(t, dataBytes, 0.0, "a flushed store must occupy disk")

	bytesPerSample := dataBytes / rows
	projected := projectDiskBytes(bytesPerSample, seriesBudget, retentionWindow, vitalsCadence)
	reference := projectDiskBytes(referenceBytesPerSample, seriesBudget, retentionWindow, vitalsCadence)

	t.Logf("EVIDENCE Q4 %s: %d series x %d samples = %.0f rows in %.1f MB -> %.3f B/sample",
		version, series, minutes, rows, dataBytes/megabyte, bytesPerSample)
	t.Logf("EVIDENCE Q4 projection at Q2's %d series over 30 d: %.2f GB measured here, %.2f GB at the store's own %.3f B/sample (budget %.2f GB)",
		seriesBudget, projected/gigabyte, reference/gigabyte, referenceBytesPerSample, float64(diskBudgetBytes)/gigabyte)
	t.Logf("EVIDENCE Q4 ratio to the store's own cost per sample: %.1fx — a short run is index-heavy, so this converges downward with series length",
		bytesPerSample/referenceBytesPerSample)
}

// parseLoadPoints reads the load points from a spec, falling back to the default scale when empty.
func parseLoadPoints(spec string) ([]int, error) {
	if strings.TrimSpace(spec) == "" {
		return defaultLoadPoints, nil
	}

	fields := strings.Split(spec, ",")
	if len(fields) < 4 {
		return nil, fmt.Errorf("%s: a fit needs at least four load points, got %d", loadPointsEnv, len(fields))
	}

	points := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a device count: %w", loadPointsEnv, field, err)
		}
		if len(points) == 0 && n <= warmupDevices {
			return nil, fmt.Errorf("%s: the first load point must sit past the %d-device warm-up, got %d",
				loadPointsEnv, warmupDevices, n)
		}
		if len(points) > 0 && n <= points[len(points)-1] {
			return nil, fmt.Errorf("%s: load points must increase, %d does not follow %d", loadPointsEnv, n, points[len(points)-1])
		}
		points = append(points, n)
	}
	return points, nil
}

// envInt reads a positive integer override and fails on a malformed one.
func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	require.NoErrorf(t, err, "%s=%q is not a number", name, raw)
	require.Positivef(t, n, "%s must be positive, got %d", name, n)
	return n
}

// deviceIDs returns n device ids unique to this run.
func deviceIDs(runID string, n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, fmt.Sprintf("%s-dev-%d", runID, i))
	}
	return ids
}

// expositionBase is the timestamp sample index 0 carries, fixed for the whole run.
var expositionBase = time.Now().Truncate(vitalsCadence).UnixMilli()

// vitalsExposition renders the sizing shape for each device as Prometheus exposition lines,
// sampleIndex cadence ticks before the run's base; the line count equals the series count.
func vitalsExposition(runID string, devices []string, sampleIndex int) string {
	const tenants = 5
	ts := expositionBase - int64(sampleIndex)*vitalsCadence.Milliseconds()

	var b strings.Builder
	for i, device := range devices {
		tenant := fmt.Sprintf("tenant-%d", i%tenants)
		seed := i * seriesPerDevice()
		emit := func(name, key, value string) {
			reading := gaugeReading(seed, sampleIndex)
			seed++
			if key == "" {
				fmt.Fprintf(&b, "%s{run_id=%q,tenant_id=%q,device_id=%q} %.1f %d\n",
					name, runID, tenant, device, reading, ts)
				return
			}
			fmt.Fprintf(&b, "%s{run_id=%q,tenant_id=%q,device_id=%q,%s=%q} %.1f %d\n",
				name, runID, tenant, device, key, value, reading, ts)
		}
		for _, dim := range vitalDims {
			emit(metricAvgName, "dim", dim)
		}
		emit(nodeAnomalyRateName, "", "")
		for _, family := range anomalyFamilies {
			emit(familyAnomalyRateName, "family", family)
		}
	}
	return b.String()
}

// gaugeReading is a bounded, drifting, slightly jittery value to a tenth of a percent, which
// compresses like a host gauge.
func gaugeReading(seed, sampleIndex int) float64 {
	phase := float64(seed%17) / 17 * 2 * math.Pi
	drift := 50 + 40*math.Sin(phase+float64(sampleIndex)/gaugeDriftSamples)
	step := float64((seed*2654435761+sampleIndex*40503)%3-1) / 10
	return math.Round((drift+step)*10) / 10
}

func ingest(t *testing.T, base, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/api/v1/import/prometheus", strings.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Lessf(t, resp.StatusCode, 300, "import should succeed, got %d", resp.StatusCode)
}

// settleSeries flushes and re-reads the series count until it reaches want or the attempts end.
func settleSeries(t *testing.T, base string, want int) int {
	t.Helper()
	var last int
	for range settleAttempts {
		forceFlush(t, base)
		last = tsdbActiveSeries(t, base)
		if last == want {
			return last
		}
		time.Sleep(settleInterval)
	}
	return last
}

// settleDisk flushes until VictoriaMetrics reports a non-zero on-disk size, then returns it.
func settleDisk(t *testing.T, base string) float64 {
	t.Helper()
	var last float64
	for range settleAttempts {
		forceFlush(t, base)
		last = promSum(scrape(t, base), "vm_data_size_bytes")
		if last > 0 {
			return last
		}
		time.Sleep(settleInterval)
	}
	return last
}

// collectedResidentBytes returns the lowest resident memory read while forced collections keep
// lowering it; exhausting the attempts returns the last reading.
func collectedResidentBytes(t *testing.T, base string) float64 {
	t.Helper()
	previous := math.Inf(1)
	for range rssSettleAttempts {
		forceCollection(t, base)
		time.Sleep(rssRefreshInterval)
		current := residentBytes(t, base)
		if current >= previous*(1-rssSettleTolerance) {
			return math.Min(current, previous)
		}
		previous = current
	}
	return previous
}

// residentBytes reads the resident memory VictoriaMetrics reports for itself, as
// of its last refresh of its own process metrics.
func residentBytes(t *testing.T, base string) float64 {
	t.Helper()
	rss, ok := promGauge(scrape(t, base), "process_resident_memory_bytes")
	require.True(t, ok, "VictoriaMetrics must report its own resident memory")
	return rss
}

// forceCollection makes the store's runtime collect before its memory is read.
// The heap profile it returns is discarded; running the collection is the point.
func forceCollection(t *testing.T, base string) {
	t.Helper()
	require.Equal(t, http.StatusOK, get(t, base+"/debug/pprof/heap?gc=1").status)
}

func forceFlush(t *testing.T, base string) {
	t.Helper()
	require.Equal(t, http.StatusOK, get(t, base+"/internal/force_flush").status)
}

// scrape returns VictoriaMetrics' own /metrics exposition.
func scrape(t *testing.T, base string) string {
	t.Helper()
	got := get(t, base+"/metrics")
	require.Equal(t, http.StatusOK, got.status)
	return got.body
}

// tsdbActiveSeries reads VictoriaMetrics' own count of the series it holds.
func tsdbActiveSeries(t *testing.T, base string) int {
	t.Helper()
	got := get(t, base+"/api/v1/status/tsdb")
	require.Equal(t, http.StatusOK, got.status)

	var status struct {
		Data struct {
			TotalSeries int `json:"totalSeries"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.body), &status))
	return status.Data.TotalSeries
}

type response struct {
	status int
	body   string
}

func get(t *testing.T, url string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return response{status: resp.StatusCode, body: string(body)}
}
