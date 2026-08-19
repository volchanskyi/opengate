package rules

import (
	"sort"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// Resolving a rule for one machine.
//
// The ordering — machine, then its site, then its customer, then the tenant,
// then what shipped — is not decided here. It lives in internal/settings, so it
// exists once and cannot drift between the things that depend on it. What lives
// here is which binding speaks for a rung when several could, and the values
// themselves, which stay next to the rule that declares what they may be.
//
// Each parameter resolves on its own. Contoso setting a five-minute sustain
// across the estate and then raising one file server's threshold gets both: the
// server's threshold and the customer's sustain. Resolving the whole binding as
// a unit would silently drop the sustain, which is the kind of loss nobody
// notices until an alert does not arrive.

// Resolve returns the rule as it applies to one machine, ready for the wire.
// Bindings belonging to another customer are ignored, so one customer's numbers
// never reach another's machines even inside a single tenant.
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
		// A rule version that narrowed its range inherits values outside it. The
		// nearest allowed one goes on the wire: dropping the customer's number
		// reverts an estate to a default nobody asked for, and sending it puts a
		// value on the machine the rule's author refused. Either way the rule
		// keeps firing, which is what going quiet would cost.
		nearest, _ := bounds.Nearest(resolved)
		return nearest
	}

	// A rule may be written against a name from before the vitals rename. It
	// resolves to the dimension the fleet actually collects, so the rule keeps
	// firing and nothing downstream sees two names for one reading.
	metric, ok := protocol.CanonicalRuleMetric(def.Metric)
	if !ok {
		metric = def.Metric
	}

	return protocol.ThresholdRule{
		ID:          def.ID,
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

// wireTerms converts a definition's extra conditions to their wire form. Terms
// are part of a rule's shape rather than its numbers, so they are not tunable
// and simply carry through.
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

// applicableBindings keeps the bindings that are for this rule, this customer,
// and a machine carrying these tags — sorted so the one that speaks for a rung
// is the first one found there.
//
// The order within a rung is: a targeted binding before the rung's blanket one
// (naming the machines you mean is more specific than naming none), then the
// operator's precedence, then the binding id. The id is the last resort and only
// reachable if two selectors were somehow stored at one rung with one
// precedence, which the database refuses; it is here so resolution is an answer
// in every case rather than a function of the order rows came back in.
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

// applies reports whether one binding has anything to say about this machine.
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

// speaksFirst is the order two applicable bindings are read in.
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

// paramOverrides gives internal/settings one value per rung: the first binding
// at that rung that sets this parameter, in the order applicableBindings
// established.
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

// DecidedBy names the rung a parameter's value came from and, in words, the
// tuned value that decided it.
//
// It is the answer to "why is this machine at 95?", and it walks the same
// ordering Resolve does rather than a second one — a screen that explained a
// value the delivery path did not produce would be worse than no explanation at
// all.
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

// describeBinding says what a tuned value is aimed at, in an operator's words.
func describeBinding(b Binding) string {
	rung := string(levelWord(b.Level))
	if b.Selector.IsEmpty() {
		return "set on this machine's " + rung
	}
	return "set on this machine's " + rung + ", for machines labelled " + DescribeSelector(b.Selector)
}

// levelWord is how a rung is named to a person, which is not always how it is
// named in the code: what the code calls an organization is, to whoever reads
// this screen, a customer.
func levelWord(level settings.Level) string {
	switch level {
	case settings.LevelDevice:
		return "machine"
	case settings.LevelSite:
		return "office"
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
