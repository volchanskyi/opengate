package rules

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/settings"
)

const (
	// A selector is a targeting aid: a handful of exact matches is the whole of it.
	maxSelectorTags = 8
	// Both tag bounds exist because selectors are stored as jsonb.
	maxSelectorKeyLen   = 64
	maxSelectorValueLen = 128
	// No rule declares more than the grammar's handful of tunable fields.
	maxBindingParams = 8
)

var (
	// ErrParamNotTunable means the rule does not offer that parameter.
	ErrParamNotTunable = errors.New("parameter is not tunable on this rule")
	// ErrParamOutOfBounds means the value is outside what the rule allows.
	ErrParamOutOfBounds = errors.New("parameter outside the rule's declared bounds")
	// ErrRuleMismatch means the binding names a different rule than its definition.
	ErrRuleMismatch = errors.New("binding names a different rule")
	// ErrInvalidLevel means the binding is filed against a rung nothing can be stored on.
	ErrInvalidLevel = errors.New("binding level is not a rung of the tenancy ladder")
	// ErrInvalidSelector means the selector is outside its bounds.
	ErrInvalidSelector = errors.New("selector is outside its bounds")
	// ErrUnknownRule means no definition in the catalogue has that id.
	ErrUnknownRule = errors.New("unknown rule")
)

// Selector is a bounded tag predicate; an empty selector covers the whole level it is filed on.
type Selector map[string]string

// IsEmpty reports whether the selector targets nothing in particular.
func (s Selector) IsEmpty() bool { return len(s) == 0 }

// Matches reports whether a device carrying tags is covered; every tag named must match exactly.
func (s Selector) Matches(tags map[string]string) bool {
	for key, want := range s {
		if tags[key] != want {
			return false
		}
	}
	return true
}

// Validate bounds the selector, which is stored as jsonb and so carries its own limits.
func (s Selector) Validate() error {
	if len(s) > maxSelectorTags {
		return fmt.Errorf("%w: %d tags, at most %d", ErrInvalidSelector, len(s), maxSelectorTags)
	}
	for key, value := range s {
		if key == "" {
			return fmt.Errorf("%w: a tag key cannot be empty", ErrInvalidSelector)
		}
		if len(key) > maxSelectorKeyLen {
			return fmt.Errorf("%w: tag key %q is longer than %d characters", ErrInvalidSelector, key, maxSelectorKeyLen)
		}
		if len(value) > maxSelectorValueLen {
			return fmt.Errorf("%w: tag %q's value is longer than %d characters", ErrInvalidSelector, key, maxSelectorValueLen)
		}
	}
	return nil
}

// Binding is one customer's parameter override for one rule, filed on one tenancy rung and
// optionally narrowed to the machines a tag selector picks out.
type Binding struct {
	ID uuid.UUID
	// OrganizationID is read by resolution so one customer's numbers never reach another's machines.
	OrganizationID uuid.UUID
	RuleID         string
	Level          settings.Level
	LevelKey       uuid.UUID
	Selector       Selector
	// Precedence breaks a tie between selectors matching one machine at one rung; higher wins.
	Precedence int
	Params     map[string]float64
	UpdatedBy  string
}

// Device is what resolution needs to know about one machine: its tenancy position, tags and estate.
type Device struct {
	Scope settings.Scope
	Tags  map[string]string
	// FleetSize sizes a rollout stage; zero means the estate could not be counted, costing a floor.
	FleetSize int
}

// ValidateBinding refuses a binding with an untunable or out-of-bounds parameter, a level that
// names no rung, or a selector beyond its limits.
func ValidateBinding(def Definition, b Binding) error {
	if b.RuleID != def.ID {
		return fmt.Errorf("%w: binding names %q, rule is %q", ErrRuleMismatch, b.RuleID, def.ID)
	}
	if !storableLevel(b.Level) {
		return fmt.Errorf("%w: %s", ErrInvalidLevel, b.Level)
	}
	if err := b.Selector.Validate(); err != nil {
		return err
	}
	if len(b.Params) > maxBindingParams {
		return fmt.Errorf("%w: %d parameters, at most %d", ErrParamNotTunable, len(b.Params), maxBindingParams)
	}

	for _, name := range sortedParamNames(b.Params) {
		bounds, ok := def.Tunable[name]
		if !ok {
			return fmt.Errorf("%w: %s does not offer %q", ErrParamNotTunable, def.ID, name)
		}
		if value := b.Params[name]; !bounds.Contains(value) {
			return fmt.Errorf("%w: %s %v is outside %s", ErrParamOutOfBounds, name, value, bounds)
		}
	}
	return nil
}

// Pack is the lookup a write validates against: whatever holds the definitions this server runs.
type Pack interface {
	Lookup(id string) (Definition, bool)
	All() []Definition
}

// ValidateBindingAgainst looks the rule up first, for a write path holding a pack and a rule id.
func ValidateBindingAgainst(cat Pack, b Binding) error {
	def, ok := cat.Lookup(b.RuleID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownRule, b.RuleID)
	}
	return ValidateBinding(def, b)
}

// The shipped floor applies when no rung carries a value, so nothing is stored there.
func storableLevel(level settings.Level) bool {
	switch level {
	case settings.LevelDevice, settings.LevelSite, settings.LevelOrganization, settings.LevelTenant:
		return true
	case settings.LevelShipped:
		return false
	default:
		return false
	}
}

// Sorted names make the same bad binding fail on the same parameter.
func sortedParamNames(params map[string]float64) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
