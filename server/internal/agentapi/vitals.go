package agentapi

// The vitals contract: the dimension names an agent may write to central
// VictoriaMetrics, and the cap on how many series one device may occupy there.
//
// Central cardinality is the binding constraint on this store, and a dim name
// arrives as untrusted input on the wire. Copying it into a VM label would make
// the number of central series a property of what agents send rather than of
// what the fleet agreed to send, and one misbehaving or compromised agent could
// then multiply the whole tenant's series count. So the vocabulary is fixed
// here, in the server, and a name outside it is dropped and counted.
//
// This list mirrors the agent's `store_sink::central_dim_names`, which builds it
// from the same series order. Cross-language golden fixtures
// (testdata/golden/control_agent_metric_window_host_metrics.bin) pin the pair
// together, so the two cannot drift without a failing test.

// vitalDims is every dimension of opengate_edge_metric_avg a device may write,
// in the order a window carries them: each gauge's average, followed by its
// window maximum where a within-minute spike is the signal, then the stall
// vitals and the disk-performance vitals. A minute's average hides a
// five-second freeze; its maximum does not.
//
// The five stall.* dims are the share of the last 60 s that tasks spent stalled
// on a resource, read from the kernel's own pressure accounting. The three disk
// dims at the end answer how *slow* the disks were rather than how full: service
// time per I/O on the worst device, its within-minute peak, and how many I/Os
// were outstanding on average. Both sets ship from a Linux agent; a platform
// without the kernel sources of its own — and a containerized agent, whose
// host-wide disk counters are its neighbours' — reports them as unsupported and
// sends nothing, so their absence is a stated gap rather than a run of zeroes
// that reads as calm.
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
	"stall.cpu.some",
	"stall.mem.some",
	"stall.mem.full",
	"stall.io.some",
	"stall.io.full",
	"disk.await_ms",
	"disk.await_ms.max",
	"disk.queue_depth",
}

// anomalyFamilies is every metric family a health summary may carry a rate for.
// A family name arrives on the wire as a string and is copied into a central
// label, so the same reasoning that fixes vitalDims fixes this list: how many
// series a device's summary occupies is a property of what the fleet agreed to
// send, not of what an agent chooses to send. A name outside the list is
// dropped and counted.
//
// This list mirrors the family set the agent's sampler reports and the one the
// load generator emits (`defaultFamilies` in server/tests/loadtest/soak.go);
// the cross-language golden fixture testdata/golden/control_agent_health_summary.bin
// pins the pair together.
var anomalyFamilies = []string{
	"cpu",
	"mem",
	"disk",
	"net",
	"proc",
}

// anomalySeriesPerDevice counts the series a device's health summary occupies:
// one node-wide anomaly rate plus one rate per listed metric family.
const anomalySeriesPerDevice = 1 + 5

// anomalyFamilySet is anomalyFamilies as a lookup, built once at startup so the
// ingest path costs a map read per family rather than a scan.
var anomalyFamilySet = func() map[string]bool {
	set := make(map[string]bool, len(anomalyFamilies))
	for _, family := range anomalyFamilies {
		set[family] = true
	}
	return set
}()

// isAnomalyFamily reports whether a family name is one the fleet agreed to store.
func isAnomalyFamily(name string) bool { return anomalyFamilySet[name] }

// vitalSeriesCap is the most central series one device may occupy. The count is
// the whole cardinality budget per device, so it is a fixed number here rather
// than a consequence of whatever the fleet happens to emit: crossing it is a
// schema decision, not an accident. A Linux device now writes exactly this many,
// so the next vital of any kind is a decision about the cap itself.
const vitalSeriesCap = 24

// vitalDimSet is vitalDims as a lookup, built once at startup so the ingest path
// costs a map read per dim rather than a scan.
var vitalDimSet = func() map[string]bool {
	set := make(map[string]bool, len(vitalDims))
	for _, dim := range vitalDims {
		set[dim] = true
	}
	return set
}()

// isVitalDim reports whether a dimension name is one the fleet agreed to store.
func isVitalDim(name string) bool { return vitalDimSet[name] }
