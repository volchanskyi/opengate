package rules

import (
	"bytes"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
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
