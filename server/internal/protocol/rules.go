package protocol

// The alert-rule metric vocabulary: what a rule is allowed to watch, and what a
// rule's declared metric name means.
//
// Rules already pushed to the fleet name the pre-rename dimensions `mem.used`
// and `disk.used`, while the vitals the agent collects are `mem.used_percent`
// and `disk.used_percent`. Rather than break every rule in flight, the old names
// are accepted as aliases and resolved to the canonical one on the way in, so
// one reading is only ever recorded under one name.
//
// This mirrors the Rust `RULE_METRICS` / `RULE_METRIC_ALIASES` /
// `canonical_rule_metric` in mesh-protocol. The reverse golden
// go_control_push_alert_rules.bin is generated from the lists below and decoded
// by the Rust harness, which asserts every name resolves and that what it
// resolved to is exactly its own vocabulary — so the two cannot drift apart
// without a failing test.

// RuleMetrics is every metric name a rule may watch, canonical. These are the
// vitals names — the dimensions the fleet agreed to collect — so a rule can only
// ever watch something that is actually being read.
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

// RuleMetricAliases maps a metric name a rule may still be written in to the
// canonical name it means.
var RuleMetricAliases = map[string]string{
	"mem.used":  "mem.used_percent",
	"disk.used": "disk.used_percent",
}

// ruleMetricSet is RuleMetrics as a lookup, built once at startup so resolution
// costs a map read rather than a scan.
var ruleMetricSet = func() map[string]bool {
	set := make(map[string]bool, len(RuleMetrics))
	for _, name := range RuleMetrics {
		set[name] = true
	}
	return set
}()

// CanonicalRuleMetric resolves a rule's declared metric name to its canonical
// vitals name. The second return is false for a name outside the vocabulary —
// a rule naming one never fires and is counted unsupported, never silently
// skipped, and a breach reported under one is dropped rather than allowed to
// become an unbounded metric label.
func CanonicalRuleMetric(name string) (string, bool) {
	if ruleMetricSet[name] {
		return name, true
	}
	canonical, ok := RuleMetricAliases[name]
	return canonical, ok
}
