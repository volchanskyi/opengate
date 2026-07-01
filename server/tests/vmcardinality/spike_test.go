// Package vmcardinality is a feasibility measurement: it ingests a
// representative multi-tenant telemetry schema into a throwaway VictoriaMetrics
// and measures the resulting active-series count, so the central-store
// cardinality budget is grounded in a real measurement rather than a guess.
//
// Two schema decisions are under test and demonstrated here:
//
//   - Central store keeps ONE aggregate (avg) per dimension, not four
//     (min/max/avg/last). Each aggregate is its own series, so storing all four
//     would ~4x the active-series count; min/max/last stay agent-local.
//   - Per-entity expansion (per-core CPU, per-disk, per-filesystem,
//     per-interface) is CAPPED, so a single large host cannot drive unbounded
//     cardinality. Central growth is then ~linear in agent count, not host size.
//
// The per-dimension and per-entity-cap counts below are representative and are
// refined once the agent sampler's real dimension set is benchmarked on
// hardware; the harness re-runs to re-ratify the budget when they change.
package vmcardinality

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

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

// Health-summary series emitted per agent: one node-wide anomaly rate plus one
// rate per detected metric family.
const (
	nodeAnomalyRateSeries = 1
	familyCount           = 5 // cpu, mem, disk, net, proc
)

// Avg-only metric dimensions per family. Scalar dims are one series each;
// per-entity dims are multiplied by the (capped) entity count.
const (
	cpuScalarDims     = 4 // usage, user, system, iowait
	cpuPerCoreDims    = 1 // usage, per core
	memScalarDims     = 5 // used, available, cached, buffers, swap_used
	diskPerDeviceDims = 3 // read_bytes, write_bytes, io_util, per block device
	fsPerMountDims    = 1 // used_pct, per mounted filesystem
	netPerIfaceDims   = 4 // rx_bytes, tx_bytes, rx_errors, tx_errors, per interface
)

// Aggregates stored centrally per dimension: the locked decision (avg only) vs.
// the rejected alternative (min/max/avg/last).
const (
	aggregatesAvgOnly = 1
	aggregatesAllFour = 4
)

// Per-entity caps: the most series any single host may contribute for each
// expandable entity, regardless of the host's real hardware count.
const (
	maxCores       = 16
	maxDisks       = 8
	maxFilesystems = 12
	maxInterfaces  = 8
)

// hostProfile is a representative host after per-entity caps are applied.
type hostProfile struct {
	name        string
	cores       int
	disks       int
	filesystems int
	interfaces  int
}

// cappedProfile clamps raw host counts to the per-entity caps, modelling the
// agent-side cap so no single host can blow up central cardinality.
func cappedProfile(name string, cores, disks, filesystems, interfaces int) hostProfile {
	return hostProfile{
		name:        name,
		cores:       min(cores, maxCores),
		disks:       min(disks, maxDisks),
		filesystems: min(filesystems, maxFilesystems),
		interfaces:  min(interfaces, maxInterfaces),
	}
}

// metricDimsPerAgent is the count of distinct avg dimensions (excluding the
// health-summary rates), before the central-aggregate multiplier.
func metricDimsPerAgent(p hostProfile) int {
	return cpuScalarDims + cpuPerCoreDims*p.cores +
		memScalarDims +
		diskPerDeviceDims*p.disks +
		fsPerMountDims*p.filesystems +
		netPerIfaceDims*p.interfaces
}

func healthDimsPerAgent() int { return nodeAnomalyRateSeries + familyCount }

// avgOnlySeriesPerAgent is the central active-series count per agent under the
// locked avg-only decision.
func avgOnlySeriesPerAgent(p hostProfile) int {
	return healthDimsPerAgent() + metricDimsPerAgent(p)*aggregatesAvgOnly
}

// allFourSeriesPerAgent is the count had all four aggregates been centralised —
// the rejected alternative, kept here to quantify what the decision avoids.
func allFourSeriesPerAgent(p hostProfile) int {
	return healthDimsPerAgent() + metricDimsPerAgent(p)*aggregatesAllFour
}

var (
	smallProfile   = cappedProfile("small", 4, 1, 2, 1)
	typicalProfile = cappedProfile("typical", 8, 2, 3, 2)
	// largeProfile intentionally exceeds every cap to prove clamping.
	largeProfile = cappedProfile("large", 64, 20, 40, 16)
)

// TestSeriesModel pins the cardinality arithmetic and the per-entity caps so a
// schema change that would silently inflate central series fails loudly here,
// before any VM is involved.
func TestSeriesModel(t *testing.T) {
	tests := []struct {
		p           hostProfile
		wantAvg     int
		wantAllFour int
	}{
		// small: dims = 4+4 +5 +3 +2 +4 = 22 → avg 6+22=28, allfour 6+88=94.
		{smallProfile, 28, 94},
		// typical: dims = 4+8 +5 +6 +3 +8 = 34 → avg 6+34=40, allfour 6+136=142.
		{typicalProfile, 40, 142},
		// large (capped 16/8/12/8): dims = 4+16 +5 +24 +12 +32 = 93 → avg 99, allfour 6+372=378.
		{largeProfile, 99, 378},
	}
	for _, tt := range tests {
		t.Run(tt.p.name, func(t *testing.T) {
			require.Equal(t, tt.wantAvg, avgOnlySeriesPerAgent(tt.p), "avg-only series/agent")
			require.Equal(t, tt.wantAllFour, allFourSeriesPerAgent(tt.p), "all-four series/agent")
			// The avoided cost is a ~4x-class multiplier on the metric dims.
			require.Greater(t, allFourSeriesPerAgent(tt.p), avgOnlySeriesPerAgent(tt.p))
		})
	}

	// Caps clamp a pathological host so central cardinality stays bounded.
	clamped := cappedProfile("pathological", 1000, 1000, 1000, 1000)
	require.Equal(t, maxCores, clamped.cores)
	require.Equal(t, maxDisks, clamped.disks)
	require.Equal(t, maxFilesystems, clamped.filesystems)
	require.Equal(t, maxInterfaces, clamped.interfaces)

	// Even a fully-capped host stays within budget at the reference fleet size.
	require.LessOrEqual(t, avgOnlySeriesPerAgent(largeProfile)*referenceAgents, seriesBudget,
		"a capped large host must still fit the central budget at reference scale")
}

// TestVMCardinalitySpike ingests the avg-only schema for the reference fleet
// into a real VictoriaMetrics and asserts the measured active-series count
// matches the model and fits the budget — the empirical half of the gate.
func TestVMCardinalitySpike(t *testing.T) {
	base := testvm.BaseURL(t)
	p := typicalProfile
	perAgent := avgOnlySeriesPerAgent(p)

	// Stage 1: first 100 agents. The generator's line count must equal the
	// model, and VM's active-series count must equal the lines ingested.
	lines100, n100 := generate(p, 0, 100)
	require.Equal(t, 100*perAgent, n100, "generator must match the series model")
	ingest(t, base, lines100)
	require.Equal(t, n100, measureTotalSeries(t, base, n100))

	// Stage 2: scale to the reference fleet (add the remaining agents).
	lines500, n400 := generate(p, 100, referenceAgents)
	require.Equal(t, (referenceAgents-100)*perAgent, n400)
	ingest(t, base, lines500)

	total := referenceAgents * perAgent
	require.Equal(t, total, measureTotalSeries(t, base, total),
		"measured VM active series must match the avg-only model at reference scale")
	require.LessOrEqualf(t, total, seriesBudget,
		"avg-only cardinality (%d) must fit the central budget (%d)", total, seriesBudget)

	// Evidence: what the avg-only decision buys versus centralising all four.
	allFour := referenceAgents * allFourSeriesPerAgent(p)
	t.Logf("EVIDENCE avg-only: %d series/agent (%s host) -> %d @%d agents (budget %d) PASS",
		perAgent, p.name, total, referenceAgents, seriesBudget)
	t.Logf("EVIDENCE all-four: %d series/agent -> %d @%d agents (~%.1fx, over budget) -> avg-only justified",
		allFourSeriesPerAgent(p), allFour, referenceAgents, float64(allFour)/float64(total))
}

// generate returns Prometheus exposition lines for agents in [start,end) of the
// given profile, spread across a handful of tenants, plus the number of distinct
// series produced. Metric name + label set is unique per (agent, dimension,
// entity), so the line count equals the distinct active-series count.
func generate(p hostProfile, start, end int) (string, int) {
	const tenants = 5
	ts := time.Now().UnixMilli()
	var b strings.Builder
	count := 0
	emit := func(name, device, org, extraKey, extraVal string) {
		if extraKey == "" {
			fmt.Fprintf(&b, "%s{org_id=%q,device_id=%q} 1 %d\n", name, org, device, ts)
		} else {
			fmt.Fprintf(&b, "%s{org_id=%q,device_id=%q,%s=%q} 1 %d\n", name, org, device, extraKey, extraVal, ts)
		}
		count++
	}
	for a := start; a < end; a++ {
		device := fmt.Sprintf("dev-%d", a)
		org := fmt.Sprintf("org-%d", a%tenants)

		emit("es_node_anomaly_rate", device, org, "", "")
		for _, fam := range []string{"cpu", "mem", "disk", "net", "proc"} {
			emit("es_family_anomaly_rate", device, org, "family", fam)
		}
		for _, name := range []string{"es_cpu_usage_avg", "es_cpu_user_avg", "es_cpu_system_avg", "es_cpu_iowait_avg"} {
			emit(name, device, org, "", "")
		}
		for c := range p.cores {
			emit("es_cpu_core_usage_avg", device, org, "core", fmt.Sprintf("%d", c))
		}
		for _, name := range []string{"es_mem_used_avg", "es_mem_available_avg", "es_mem_cached_avg", "es_mem_buffers_avg", "es_mem_swap_used_avg"} {
			emit(name, device, org, "", "")
		}
		for d := range p.disks {
			dev := fmt.Sprintf("disk%d", d)
			for _, name := range []string{"es_disk_read_bytes_avg", "es_disk_write_bytes_avg", "es_disk_io_util_avg"} {
				emit(name, device, org, "block_device", dev)
			}
		}
		for f := range p.filesystems {
			emit("es_fs_used_pct_avg", device, org, "mount", fmt.Sprintf("mnt%d", f))
		}
		for i := range p.interfaces {
			iface := fmt.Sprintf("eth%d", i)
			for _, name := range []string{"es_net_rx_bytes_avg", "es_net_tx_bytes_avg", "es_net_rx_errors_avg", "es_net_tx_errors_avg"} {
				emit(name, device, org, "iface", iface)
			}
		}
	}
	return b.String(), count
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

// measureTotalSeries flushes VM and reads data.totalSeries, retrying (in the
// same goroutine, so -race stays clean) until the count reaches want or the
// budget of attempts is spent — then returns the last reading for the caller to
// assert on, so a mismatch fails loudly rather than skips.
func measureTotalSeries(t *testing.T, base string, want int) int {
	t.Helper()
	var last int
	for range 25 {
		forceFlush(t, base)
		last = totalSeries(t, base)
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

func totalSeries(t *testing.T, base string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/v1/status/tsdb", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var status struct {
		Data struct {
			TotalSeries int `json:"totalSeries"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	return status.Data.TotalSeries
}
