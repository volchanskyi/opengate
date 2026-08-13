package rules

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Loading a pack: what the catalogue refuses, and why each refusal is at load
// time rather than at the moment a rule would have fired.

// LoadCatalogue parses and validates a pack. A nil lock skips the immutability
// check, which is what a fixture wants; the shipped pack always carries one.
func LoadCatalogue(data []byte, lock Lock) (*Catalogue, error) {
	defs, err := parseDefinitions(data)
	if err != nil {
		return nil, err
	}

	cat := &Catalogue{byID: make(map[string]Definition, len(defs))}
	seen := make(map[string]bool, len(defs))
	var total uint64

	for i, def := range defs {
		if err := validateDefinition(def, i); err != nil {
			return nil, err
		}
		if seen[def.Key()] {
			return nil, fmt.Errorf("rule %s: duplicate (rule_id, version)", def.Key())
		}
		seen[def.Key()] = true
		if _, exists := cat.byID[def.ID]; exists {
			return nil, fmt.Errorf("rule %s: duplicate rule id", def.ID)
		}
		if err := checkLock(def, lock); err != nil {
			return nil, err
		}

		cost := RuleCost(def)
		if cost > MaxRuleCost {
			return nil, fmt.Errorf(
				"rule %s: evaluation cost %d readings exceeds the per-rule budget of %d; narrow its window",
				def.Key(), cost, MaxRuleCost)
		}
		total += cost

		cat.byID[def.ID] = def
		cat.order = append(cat.order, def.ID)
	}

	if total > MaxCatalogueCost {
		return nil, fmt.Errorf(
			"catalogue asks every endpoint to hold %d readings, over the per-agent budget of %d",
			total, MaxCatalogueCost)
	}

	sort.Strings(cat.order)
	return cat, nil
}

// parseDefinitions decodes the pack with unknown fields refused, so a typo is a
// load failure rather than a rule that silently does something else.
func parseDefinitions(data []byte) ([]Definition, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var file catalogueFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse catalogue: %w", err)
	}
	if len(file.Rules) == 0 {
		return nil, fmt.Errorf("parse catalogue: no rules")
	}
	return file.Rules, nil
}

// checkLock refuses a definition whose meaning changed without its version
// changing. A key the lock does not carry is a new rule or a new version, which
// is exactly how a definition is allowed to change.
func checkLock(def Definition, lock Lock) error {
	if lock == nil {
		return nil
	}
	want, ok := lock[def.Key()]
	if !ok {
		return nil
	}
	got, err := def.Digest()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf(
			"rule %s: immutable definition changed; bump the version instead of editing a published one",
			def.Key())
	}
	return nil
}

// validateDefinition refuses anything the grammar cannot express or the fleet
// cannot evaluate. Position is reported when the rule has no usable id.
func validateDefinition(def Definition, index int) error {
	where := def.ID
	if where == "" {
		where = "#" + strconv.Itoa(index)
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("rule %s: "+format, append([]any{where}, args...)...)
	}

	if def.ID == "" {
		return fail("id is required")
	}
	if len(def.ID) > maxRuleIDLen || !validRuleID(def.ID) {
		return fail("id must be lower-case letters, digits and dashes, at most %d characters", maxRuleIDLen)
	}
	if def.Version < 1 {
		return fail("version must be 1 or greater")
	}
	if def.Summary == "" {
		return fail("summary is required — a rule must say what it is for")
	}

	if err := validateCondition(def.Metric, def.ComparatorName, def.PredicateName, fail); err != nil {
		return err
	}
	for i, term := range def.All {
		if err := validateCondition(term.Metric, term.ComparatorName, term.PredicateName,
			func(format string, args ...any) error {
				return fail("term %d: "+format, append([]any{i}, args...)...)
			}); err != nil {
			return err
		}
	}

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

	return validateTunable(def, fail)
}

// validateCondition checks one side of a rule against the vocabularies.
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

// validateTunable checks that every declared parameter is one the grammar
// carries, that its range is a range, and that the rule's own shipped value sits
// inside it — a rule may not ship a default its own bindings would be refused.
func validateTunable(def Definition, fail func(string, ...any) error) error {
	names := make([]string, 0, len(def.Tunable))
	for name := range def.Tunable {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		bounds := def.Tunable[name]
		if !tunableFields[name] {
			return fail("tunable %q is not a parameter a binding can set", name)
		}
		if bounds.Min > bounds.Max {
			return fail("tunable %q has inverted bounds %s", name, bounds)
		}
		shipped, ok := def.ShippedParam(name)
		if !ok {
			return fail("tunable %q has no shipped value", name)
		}
		if !bounds.Contains(shipped) {
			return fail("shipped %s of %s is outside its own declared bounds %s",
				name, strconv.FormatFloat(shipped, 'g', -1, 64), bounds)
		}
	}
	return nil
}

// validRuleID reports whether id is lower-case letters, digits and dashes.
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
