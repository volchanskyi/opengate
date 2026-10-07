package rules

import (
	"sort"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// Resolve returns the rule as it applies to one machine, ready for the wire.
// Each parameter resolves on its own; another customer's bindings are ignored.
func Resolve(def Definition, device Device, bindings []Binding) protocol.ThresholdRule {
	applicable := applicableBindings(def, device, bindings)

	value := func(name string) float64 {
		shipped, _ := def.ShippedParam(name)
		bounds, tunable := def.Tunable[name]
		if !tunable {
			return shipped
		}
		overrides := paramOverrides(name, device, applicable)
		resolved, _ := settings.Resolve(device.Scope, overrides, shipped, settings.NarrowestWins)
		// A rule version that narrowed its range inherits values outside it; the nearest
		// allowed value goes on the wire.
		nearest, _ := bounds.Nearest(resolved)
		return nearest
	}

	// A pre-rename metric name resolves to the dimension the fleet collects.
	metric, ok := protocol.CanonicalRuleMetric(def.Metric)
	if !ok {
		metric = def.Metric
	}

	return protocol.ThresholdRule{
		ID: def.ID,
		// The definition's own revision; a customer's number is configuration, not a new one.
		Version:     wireVersion(def.Version),
		Severity:    def.WireSeverity(),
		Metric:      metric,
		Comparator:  def.Comparator(),
		Threshold:   value("threshold"),
		Clear:       value("clear"),
		SustainSecs: uint32(value("sustain_secs")),
		Predicate:   def.Predicate(),
		WindowSecs:  uint32(value("window_secs")),
		All:         wireTerms(def.All),
	}
}

// wireVersion returns a revision as the wire carries it, or 0 when it does not fit.
func wireVersion(version int) uint32 {
	if version < minRuleVersion || uint64(version) > maxRuleVersion {
		return 0
	}
	return uint32(version)
}

// wireTerms converts a definition's extra conditions to their wire form; terms are not tunable.
func wireTerms(terms []Term) []protocol.RuleTerm {
	if len(terms) == 0 {
		return nil
	}
	out := make([]protocol.RuleTerm, 0, len(terms))
	for _, term := range terms {
		metric, ok := protocol.CanonicalRuleMetric(term.Metric)
		if !ok {
			metric = term.Metric
		}
		out = append(out, protocol.RuleTerm{
			Metric:     metric,
			Comparator: term.Comparator(),
			Threshold:  term.Threshold,
			Clear:      term.Clear,
			Predicate:  term.Predicate(),
			WindowSecs: term.WindowSecs,
		})
	}
	return out
}

// applicableBindings keeps the bindings for this rule, customer and tags, in speaksFirst order.
func applicableBindings(def Definition, device Device, bindings []Binding) []Binding {
	out := make([]Binding, 0, len(bindings))
	for _, b := range bindings {
		if applies(def, device, b) {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return speaksFirst(out[i], out[j]) })
	return out
}

func applies(def Definition, device Device, b Binding) bool {
	if b.RuleID != def.ID || b.OrganizationID != device.Scope.OrganizationID {
		return false
	}
	if !storableLevel(b.Level) {
		return false
	}
	key, present := device.Scope.Key(b.Level)
	if !present || key != b.LevelKey {
		return false
	}
	return b.Selector.Matches(device.Tags)
}

// speaksFirst orders bindings by level, targeted before blanket, precedence, then id.
func speaksFirst(a, b Binding) bool {
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	if a.Selector.IsEmpty() != b.Selector.IsEmpty() {
		return !a.Selector.IsEmpty()
	}
	if a.Precedence != b.Precedence {
		return a.Precedence > b.Precedence
	}
	return a.ID.String() < b.ID.String()
}

// paramOverrides returns one value per rung: the first applicable binding there that sets the name.
func paramOverrides(name string, device Device, applicable []Binding) []settings.Override[float64] {
	overrides := make([]settings.Override[float64], 0, len(applicable))
	seen := make(map[settings.Level]bool, len(applicable))

	for _, b := range applicable {
		value, ok := b.Params[name]
		if !ok || seen[b.Level] {
			continue
		}
		key, present := device.Scope.Key(b.Level)
		if !present {
			continue
		}
		seen[b.Level] = true
		overrides = append(overrides, settings.Override[float64]{
			Level:   b.Level,
			ScopeID: key,
			Value:   value,
		})
	}
	return overrides
}

// DecidedBy names the rung a parameter's value came from and describes the tuned value that
// decided it, walking the same ordering as Resolve.
func DecidedBy(def Definition, device Device, bindings []Binding, name string) (settings.Level, string) {
	if _, tunable := def.Tunable[name]; !tunable {
		return settings.LevelShipped, "the value the rule ships"
	}

	for _, b := range applicableBindings(def, device, bindings) {
		if _, set := b.Params[name]; !set {
			continue
		}
		return b.Level, describeBinding(b)
	}
	return settings.LevelShipped, "the value the rule ships"
}

func describeBinding(b Binding) string {
	where := "set on this host's " + levelWord(b.Level)
	if b.Level == settings.LevelDevice {
		where = "set on this host"
	}
	if b.Selector.IsEmpty() {
		return where
	}
	return where + ", for hosts labelled " + DescribeSelector(b.Selector)
}

// levelWord names a rung as a person reads it, calling a device a host and an organization a
// customer.
func levelWord(level settings.Level) string {
	switch level {
	case settings.LevelDevice:
		return "host"
	case settings.LevelSite:
		return "site"
	case settings.LevelOrganization:
		return "customer"
	case settings.LevelTenant:
		return "platform"
	case settings.LevelShipped:
		return "shipped default"
	default:
		return "shipped default"
	}
}
