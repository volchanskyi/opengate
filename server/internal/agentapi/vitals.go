package agentapi

// vitalDims is every dimension an agent may write to central metrics, in window order.
// Dim names arrive as untrusted input, so this fixed list bounds central series cardinality.
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
// Family names arrive as untrusted input, so this fixed list bounds central series cardinality.
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

// anomalyFamilySet is anomalyFamilies as a lookup.
var anomalyFamilySet = func() map[string]bool {
	set := make(map[string]bool, len(anomalyFamilies))
	for _, family := range anomalyFamilies {
		set[family] = true
	}
	return set
}()

// isAnomalyFamily reports whether a family name is one the fleet agreed to store.
func isAnomalyFamily(name string) bool { return anomalyFamilySet[name] }

// vitalSeriesCap is the most central series one device may occupy.
const vitalSeriesCap = 24

// vitalLabels holds each vital dimension's label set, built once and shared read-only by every
// reading of it, so a reading costs no map of its own.
var vitalLabels = func() map[string]map[string]string {
	labels := make(map[string]map[string]string, len(vitalDims))
	for _, dim := range vitalDims {
		labels[dim] = map[string]string{backfillDimLabel: dim}
	}
	return labels
}()

// isVitalDim reports whether a dimension name is one the fleet agreed to store.
func isVitalDim(name string) bool {
	_, ok := vitalLabels[name]
	return ok
}
