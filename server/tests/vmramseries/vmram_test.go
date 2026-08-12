// Package vmramseries measures what an active series actually costs the central
// store, in memory and on disk, so the store is sized from a measurement rather
// than from a rule of thumb.
//
// The deliverable is a fit, not a division. Resident memory read at one load
// point and divided by the series held there charges VictoriaMetrics' fixed
// baseline to whatever series happen to be present, which overstates the
// per-series cost by the baseline divided by N — worst exactly where a test
// harness has to work, at small N. A line through four or more load points
// separates the baseline from the slope, and the slope is the number that scales
// to a fleet.
//
// The committed run measures at a small scale that finishes inside the suite's
// budget, and it always runs: the load points come from a constant, and the
// environment can only widen them. The fleet-scale figures in the program's
// measurement record were taken with this same harness at
// OPENGATE_VMRAM_DEVICES=2000,3000,4000,5000.
//
// Nothing here changes a limit or a manifest. The number goes to the project
// owner, who decides what to do with it.
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

// The fleet this store is being sized for, and the budgets the measurement is
// read against. Q2 is derived — the per-device cap times the fleet — so the two
// can never drift; Q3 and Q4 are ceilings the measurement is compared to, not
// assertions on it. Exceeding Q3 does not fail this package: it hands the
// project owner a sizing decision.
const (
	// Sizes are decimal throughout, the units the budgets and the sizing table
	// are stated in — a figure compared against a differently-based one is a
	// 5 % error nobody notices.
	kilobyte = 1_000
	megabyte = 1_000_000
	gigabyte = 1_000_000_000

	fleetAgents     = 5_000
	seriesBudget    = 120_000        // Q2: active series at fleet scale
	ramBudgetBytes  = 400 * megabyte // Q3: total VictoriaMetrics memory
	diskBudgetBytes = 2 * gigabyte   // Q4: 30 d on disk at fleet scale

	retentionWindow = 30 * 24 * time.Hour
	vitalsCadence   = 60 * time.Second

	// referenceBytesPerSample is the central store's own cost per sample,
	// measured on the live deployment as data size over rows added. It is the
	// cross-check for what this harness measures, not its source: a divergence of
	// more than about 2x means the harness is writing the wrong shape.
	referenceBytesPerSample = 0.316
)

// vitalDims is every dimension of opengate_edge_metric_avg the sizing shape
// assumes: the cross-platform gauges with a window maximum where a within-minute
// spike is the signal, the disk-performance trio, and the stall vitals. A
// capacity plan must not assume the cheaper platform mix, so the harness writes
// the Linux-only vitals too — a device that supplies them all is what the fleet
// is sized for.
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

// warmupDevices are written and settled before the first load point, and their
// reading is evidence rather than a point on the line.
//
// VictoriaMetrics allocates lazily. Over its first writes, resident memory is
// caches and buffers being claimed for the first time — a fixed cost arriving
// late, not the cost of the series that happen to be arriving with it. Measured
// from cold, that ramp lands in the slope: a fit whose first point sits inside it
// answers several times the marginal cost, which is the same mistake as dividing
// by N, made more expensively. Warming the store puts every load point on the far
// side of the ramp, which is what separating the fixed baseline from the
// per-series slope actually requires.
const warmupDevices = 1_000

// The load points, as total device counts. Four is the minimum that makes a fit
// worth more than a line through two readings, and the default scale is chosen so
// the memory the series cost is large against the garbage collector's noise while
// the whole package still finishes in seconds.
const loadPointsEnv = "OPENGATE_VMRAM_DEVICES"

var defaultLoadPoints = []int{2000, 2600, 3200, 3800, 4400, 5000}

// The disk half needs the opposite shape: fewer series, each carrying a real
// run of samples at the cadence, because cost per sample measured over
// one-sample series measures the index and nothing else.
const (
	diskDevicesEnv     = "OPENGATE_VMRAM_DISK_DEVICES"
	diskMinutesEnv     = "OPENGATE_VMRAM_DISK_MINUTES"
	defaultDiskDevices = 200
	defaultDiskMinutes = 60
)

// vmArgs pin VictoriaMetrics' memory budget instead of letting it default to a
// share of whatever the host has. Per-series cost is a property of the build and
// its configuration, so a figure that moved between a laptop and a CI runner
// would not be a measurement of anything.
var vmArgs = []string{"-memory.allowedBytes=1073741824"}

// How resident memory is read at a load point. Two properties of the process
// being measured decide this.
//
// VictoriaMetrics refreshes the process metrics it reports about itself once a
// second, so two scrapes taken closer together than that return one number
// twice. A reading assembled from them describes the refresh interval, not the
// store, and its steadiness is not evidence that anything has settled.
//
// The Go runtime frees what an import allocated only when it collects. Left
// alone, resident memory holds a plateau of garbage that is perfectly stable
// and unrelated to the series held — which is how a larger load point reads
// smaller than the one before it, and takes the fit down with it.
//
// So a load point's reading is taken after forcing collection, repeatedly,
// until it stops falling. One collection is not always enough: the runtime
// returns pages over several cycles, and the residue of the warm-up drains
// across the early points. What this converges on is the memory the store needs
// to hold what it holds, measured in the same runtime state at every load
// point, which is the comparability a line through those points depends on.
const (
	rssRefreshInterval = 1200 * time.Millisecond
	rssSettleAttempts  = 12
	rssSettleTolerance = 0.01
)

// gaugeDriftSamples is how many cadence ticks a synthetic gauge takes to swing
// through a radian — half an hour at the vitals cadence, which is the timescale a
// host's CPU or memory actually wanders on.
const gaugeDriftSamples = 30

// settleAttempts and settleInterval bound the wait for VictoriaMetrics to make
// what was just written visible in its own accounting. Exhausting them returns
// the last reading so the caller's assertion fails loudly on the real number.
const (
	settleAttempts = 40
	settleInterval = 250 * time.Millisecond
)

// TestRAMPerActiveSeriesFit is Q3: the marginal memory cost of one active
// series, fitted over the load points, on a VictoriaMetrics that holds nothing
// else.
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

		// At the top of the load, memory is also read with the import's garbage
		// still in it — the figure a pod is sized from. It is taken a refresh
		// interval after the import so the number belongs to this load point
		// rather than to the last one the cache saw, and it is deliberately not a
		// point on the line: what the runtime happens to be holding at the moment
		// of a scrape is not a property of the series held.
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

// TestDiskPerSampleAtVitalsCadence is Q4: bytes on disk per stored sample,
// measured over series that carry a real run of samples at the vitals cadence,
// and projected to the fleet's 30 d.
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

	// Every write covers the same series set one cadence tick further back, so
	// the store ends up with series of real length rather than a wall of
	// one-sample series whose bytes are all index.
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

// parseLoadPoints reads the load points from a spec, falling back to the
// always-on scale when it is empty. The environment can only widen the
// experiment; there is no value of this variable that makes the test not run.
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

// envInt reads a positive integer override, failing loudly on a malformed one
// rather than quietly measuring at the default.
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

// expositionBase is the timestamp sample index 0 carries, fixed for the whole
// run so that walking the index backwards lengthens the same series instead of
// scattering samples across shifting grids.
var expositionBase = time.Now().Truncate(vitalsCadence).UnixMilli()

// vitalsExposition renders the sizing shape for each device as Prometheus
// exposition lines, at the timestamp sampleIndex cadence ticks before the run's
// base. Metric name plus label set is unique per (device, series), so the line
// count equals the series count.
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

// gaugeReading is a bounded, drifting, slightly jittery value — what a host
// gauge looks like.
//
// Cost per sample is a compression measurement, so the shape of the values
// decides the answer as much as their number. A constant-valued harness measures
// VictoriaMetrics' best case — repeated values collapse to almost nothing — while
// full-entropy noise measures its worst; neither is the fleet. A host gauge sits
// between the two: it moves slowly and it is reported to a tenth of a percent, so
// consecutive samples usually differ by one step or not at all.
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

// settleSeries flushes VictoriaMetrics and re-reads its series count until it
// reaches want or the attempts are spent, then returns the last reading for the
// caller to assert on — a mismatch must fail loudly, never pass quietly.
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

// settleDisk flushes until VictoriaMetrics reports a non-zero on-disk size, then
// returns it. The last reading is returned either way so the caller asserts on
// the real number.
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

// collectedResidentBytes returns the VictoriaMetrics process's resident memory
// with the garbage of the import that just finished collected out of it:
// collection is forced and the memory re-read until the reading stops falling,
// and the lowest reading is what the load point is credited with.
//
// Exhausting the attempts returns the last reading rather than raising, so a
// store that never converges shows up as a point the fit cannot explain — a
// loud failure on the real numbers, which is what the assertions are for.
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
