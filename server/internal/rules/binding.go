package rules

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/settings"
)

// A binding is a customer's retuning of a rule the catalogue defines. It carries
// only numbers the rule declared tunable, and it is validated against that
// rule's own bounds on write — so a threshold nobody would want is refused at
// the moment it is typed, where an operator can still see why, rather than
// reaching five thousand endpoints and being discovered from the alerts it did
// not raise.

const (
	// maxSelectorTags bounds how many tags one selector may name. A selector is
	// a targeting aid, not an authoring surface: a handful of exact matches is
	// the whole of it.
	maxSelectorTags = 8
	// maxSelectorKeyLen and maxSelectorValueLen bound one tag. Both are stored
	// as jsonb, so they are bounded rather than left to whatever is sent.
	maxSelectorKeyLen   = 64
	maxSelectorValueLen = 128
	// maxBindingParams bounds the parameters one binding may carry. No rule
	// declares more than the grammar's handful of tunable fields.
	maxBindingParams = 8
)

// Typed failures, so an API layer can answer an operator with what is wrong
// rather than with a generic rejection.
var (
	// ErrParamNotTunable means the rule does not offer that parameter.
	ErrParamNotTunable = errors.New("parameter is not tunable on this rule")
	// ErrParamOutOfBounds means the value is outside what the rule allows.
	ErrParamOutOfBounds = errors.New("parameter outside the rule's declared bounds")
	// ErrRuleMismatch means the binding names a different rule than the
	// definition it is being validated against.
	ErrRuleMismatch = errors.New("binding names a different rule")
	// ErrInvalidLevel means the binding is filed against a rung nothing can be
	// stored on.
	ErrInvalidLevel = errors.New("binding level is not a rung of the tenancy ladder")
	// ErrInvalidSelector means the selector is outside its bounds.
	ErrInvalidSelector = errors.New("selector is outside its bounds")
	// ErrUnknownRule means no definition in the catalogue has that id.
	ErrUnknownRule = errors.New("unknown rule")
)

// Selector is a bounded tag predicate: every tag it names must match the
// device's, exactly. An empty selector names nothing and so covers the whole
// level it is filed on.
type Selector map[string]string

// IsEmpty reports whether the selector targets nothing in particular.
func (s Selector) IsEmpty() bool { return len(s) == 0 }

// Matches reports whether a device carrying tags is covered by the selector. It
// is a conjunction: every tag named must match, or the selector does not apply.
func (s Selector) Matches(tags map[string]string) bool {
	for key, want := range s {
		if tags[key] != want {
			return false
		}
	}
	return true
}

// Validate bounds the selector, which is stored as jsonb and therefore has to
// carry its own limits.
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

// Binding is one customer's parameter override for one rule, filed on one rung
// of the tenancy ladder and optionally narrowed to the machines a tag selector
// picks out.
type Binding struct {
	ID uuid.UUID
	// OrganizationID is the customer the binding belongs to. Resolution reads it
	// so one customer's numbers cannot reach another's machines even when both
	// sit inside a single tenant.
	OrganizationID uuid.UUID
	RuleID         string
	// Level and LevelKey are the rung and the id on that rung: a device, a site,
	// a customer, or the tenant.
	Level    settings.Level
	LevelKey uuid.UUID
	Selector Selector
	// Precedence breaks a tie between two selectors that both match one machine
	// at one rung. Higher wins, and it is set by the operator — the alternative
	// is a tie-break nobody can see.
	Precedence int
	Params     map[string]float64
	// UpdatedBy names whoever last set this, so a retuned threshold can be
	// traced back to the person who chose it.
	UpdatedBy string
}

// Device is what resolution needs to know about one machine: where it sits on
// the tenancy ladder, the tags a selector may pick it out by, and the size of
// the estate it belongs to.
type Device struct {
	Scope settings.Scope
	Tags  map[string]string
	// FleetSize is how many machines the customer has, which is what sizes a
	// stage a rule is still rolling out through. Zero means the estate could not
	// be counted, which costs a stage its floor and nothing else — see InStage.
	FleetSize int
}

// ValidateBinding refuses a binding the rule would not honour. Every parameter
// must be one the rule declares tunable and inside the bounds it declared, the
// rung must be a rung, and the selector must be within its limits.
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

// Pack is the lookup a write validates against: whatever holds the definitions
// this server runs. It is an interface so a caller that already has the pack
// behind its own port does not have to reach past it for the concrete type.
type Pack interface {
	Lookup(id string) (Definition, bool)
	All() []Definition
}

// ValidateBindingAgainst looks the rule up first, for the write path that has a
// pack and an operator-supplied rule id.
func ValidateBindingAgainst(cat Pack, b Binding) error {
	def, ok := cat.Lookup(b.RuleID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownRule, b.RuleID)
	}
	return ValidateBinding(def, b)
}

// storableLevel reports whether a value can be filed against this rung. The
// ladder's floor is what applies when no rung carries a value, so nothing can be
// stored there.
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

// sortedParamNames gives validation a stable order, so the same bad binding
// always fails on the same parameter.
func sortedParamNames(params map[string]float64) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
