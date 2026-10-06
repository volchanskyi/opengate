package rules

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// The rule's position is reported when it has no usable id.
func validateDefinition(def Definition, index int) error {
	where := def.ID
	if where == "" {
		where = "#" + strconv.Itoa(index)
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("rule %s: "+format, append([]any{where}, args...)...)
	}

	if err := validateIdentity(def, fail); err != nil {
		return err
	}
	if err := validateWhatItWatches(def, fail); err != nil {
		return err
	}
	if err := validateGrouping(def, fail); err != nil {
		return err
	}
	return validateTunable(def, fail)
}

func validateIdentity(def Definition, fail func(string, ...any) error) error {
	if def.ID == "" {
		return fail("id is required")
	}
	if len(def.ID) > maxRuleIDLen || !validRuleID(def.ID) {
		return fail("id must be lower-case letters, digits and dashes, at most %d characters", maxRuleIDLen)
	}
	if def.Version < minRuleVersion || uint64(def.Version) > maxRuleVersion {
		return fail("version must be between %d and %d — an alert's identity is built on it, "+
			"so a revision the wire cannot carry is one no alert can be traced back to",
			minRuleVersion, uint64(maxRuleVersion))
	}
	if def.Summary == "" {
		return fail("summary is required — a rule must say what it is for")
	}
	if _, ok := severityVocabulary[def.Severity]; !ok {
		return fail("severity %q is not one of info, warning, critical — "+
			"a queue ordered by severity cannot order a rule that states none", def.Severity)
	}
	return nil
}

func validateGrouping(def Definition, fail func(string, ...any) error) error {
	if len(def.GroupBy) == 0 {
		return fail("group_by is required — a rule must say what its alerts are about")
	}
	for _, key := range def.GroupBy {
		if !groupByVocabulary[key] {
			return fail("group_by %q is not something alerts can be grouped on", key)
		}
	}
	if def.GroupWindowSecs == 0 {
		return fail("group_window_secs must be greater than zero")
	}
	for _, kind := range def.Evidence {
		if !evidenceVocabulary[kind] {
			return fail("evidence %q is not collected", kind)
		}
	}
	for _, metric := range def.CoverageRequires {
		if _, ok := protocol.CanonicalRuleMetric(metric); !ok {
			return fail("coverage_requires %q is outside the metric vocabulary", metric)
		}
	}
	return nil
}

// A reading rule is compared on the machine, so its comparison must fit the machine's grammar;
// an event rule is matched by the machine's own log reader and states none of it.
func validateWhatItWatches(def Definition, fail func(string, ...any) error) error {
	if def.WatchesEvents() {
		return validateEventRule(def, fail)
	}
	if def.Kind != "" {
		return fail("kind %q is not a kind of rule this build evaluates", def.Kind)
	}

	if err := validateCondition(def.Metric, def.ComparatorName, def.PredicateName, fail); err != nil {
		return err
	}
	for i, term := range def.All {
		failTerm := func(format string, args ...any) error {
			return fail("term %d: "+format, append([]any{i}, args...)...)
		}
		if err := validateCondition(term.Metric, term.ComparatorName, term.PredicateName, failTerm); err != nil {
			return err
		}
	}
	return nil
}

func validateEventRule(def Definition, fail func(string, ...any) error) error {
	for name, value := range map[string]string{
		"metric":     def.Metric,
		"comparator": def.ComparatorName,
		"predicate":  def.PredicateName,
	} {
		if value != "" {
			return fail("a rule watching the machine's own words names no %s, got %q",
				name, value)
		}
	}
	if len(def.All) > 0 {
		return fail("a rule watching the machine's own words has no further conditions")
	}
	if len(def.Tunable) > 0 {
		return fail("a rule watching the machine's own words has no numbers to retune")
	}
	return nil
}

func validateCondition(metric, comparator, predicate string, fail func(string, ...any) error) error {
	if _, ok := protocol.CanonicalRuleMetric(metric); !ok {
		return fail("metric %q is outside the vocabulary the fleet collects", metric)
	}
	if _, ok := comparatorVocabulary[comparator]; !ok {
		return fail("comparator %q is not one of gt, lt, gte, lte", comparator)
	}
	if _, ok := predicateVocabulary[predicate]; !ok {
		return fail("predicate %q is outside the grammar", predicate)
	}
	return nil
}

// The shipped value of a tunable sits inside its own bounds, or its bindings would be refused.
func validateTunable(def Definition, fail func(string, ...any) error) error {
	names := make([]string, 0, len(def.Tunable))
	for name := range def.Tunable {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		bounds := def.Tunable[name]
		shipped, ok := def.ShippedParam(name)
		if !ok {
			return fail("tunable %q is not a parameter a binding can set", name)
		}
		if bounds.Min > bounds.Max {
			return fail("tunable %q has inverted bounds %s", name, bounds)
		}
		if !bounds.Contains(shipped) {
			return fail("shipped %s of %s is outside its own declared bounds %s",
				name, strconv.FormatFloat(shipped, 'g', -1, 64), bounds)
		}
	}
	return nil
}

func validRuleID(id string) bool {
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}
