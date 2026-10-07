package protocol

// RuleMetrics lists the canonical vitals names a rule may watch, mirrored by the Rust
// RULE_METRICS in mesh-protocol.
var RuleMetrics = []string{
	"cpu.total",
	"mem.used_percent",
	"disk.used_percent",
	"disk.mounts_critical",
	"net.rx_bps",
	"net.tx_bps",
	"stall.cpu.some",
	"stall.mem.some",
	"stall.mem.full",
	"stall.io.some",
	"stall.io.full",
	"disk.await_ms",
	"disk.queue_depth",
}

// RuleMetricAliases maps the pre-rename metric names in rules already pushed to the fleet to
// their canonical names.
var RuleMetricAliases = map[string]string{
	"mem.used":  "mem.used_percent",
	"disk.used": "disk.used_percent",
}

// ruleMetricSet is RuleMetrics as a map, so resolution costs one lookup.
var ruleMetricSet = func() map[string]bool {
	set := make(map[string]bool, len(RuleMetrics))
	for _, name := range RuleMetrics {
		set[name] = true
	}
	return set
}()

// CanonicalRuleMetric resolves a declared metric name to its canonical vitals name, reporting
// false for a name outside the vocabulary so it never becomes an unbounded metric label.
func CanonicalRuleMetric(name string) (string, bool) {
	if ruleMetricSet[name] {
		return name, true
	}
	canonical, ok := RuleMetricAliases[name]
	return canonical, ok
}
