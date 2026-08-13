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
		if _, tunable := def.Tunable[name]; !tunable {
			return shipped
		}
		overrides := paramOverrides(name, device, applicable)
		resolved, _ := settings.Resolve(device.Scope, overrides, shipped, settings.NarrowestWins)
		return resolved
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
		if b.RuleID != def.ID || b.OrganizationID != device.Scope.OrganizationID {
			continue
		}
		if !storableLevel(b.Level) {
			continue
		}
		key, present := device.Scope.Key(b.Level)
		if !present || key != b.LevelKey {
			continue
		}
		if !b.Selector.Matches(device.Tags) {
			continue
		}
		out = append(out, b)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
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
	})
	return out
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
