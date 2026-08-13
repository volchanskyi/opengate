package rules

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// A rule's rollout state: whether a customer gets it at all, how far it has
// reached, and the switch that stops it.
//
// This lives in Postgres rather than in the catalogue because stopping a rule
// cannot require a deploy. A rule misbehaving across an estate at three in the
// morning is answered by setting Kill, not by cutting a release.
//
// How a rule advances through its stages, and how a stop is throttled and
// propagated, belong to the rollout machinery. What is here is the state itself
// and the one reading nothing else can be built on: whether the rule is
// delivered to this customer's machines at all.

// maxCanaryGroupLen bounds the stored group label.
const maxCanaryGroupLen = 64

// ErrInvalidRollout means the stored rollout state is outside its bounds.
var ErrInvalidRollout = errors.New("invalid rollout state")

// Rollout is one customer's rollout state for one rule.
type Rollout struct {
	OrganizationID uuid.UUID
	RuleID         string
	// Enabled is whether the customer wants the rule at all.
	Enabled bool
	// CanaryGroup names the subset the rule is being tried on while it is
	// staged. It is recorded here and read by the rollout machinery.
	CanaryGroup string
	// RolloutPercent is how much of the estate the rule has reached, 0 to 100.
	RolloutPercent int
	// Kill stops the rule. It is deliberately separate from Enabled: switching a
	// rule off is a customer's ordinary choice, while a kill is an intervention,
	// and the two must be distinguishable after the fact.
	//
	// A kill is filed on the customer, which is the whole point of where it
	// lives — a customer-wide stop is not something a value set on one machine
	// can undo, because no narrower rung carries one.
	Kill           bool
	StageEnteredAt time.Time
	UpdatedAt      time.Time
	UpdatedBy      string
}

// DefaultRollout is what applies to a customer with no stored row. A rule
// nobody has configured is on and reaches the whole estate: the catalogue is
// curated, so shipping it dark would leave a new customer unmonitored and
// looking healthy. Absence of a row is "not configured", never "switched off".
func DefaultRollout(organizationID uuid.UUID, ruleID string) Rollout {
	return Rollout{
		OrganizationID: organizationID,
		RuleID:         ruleID,
		Enabled:        true,
		RolloutPercent: 100,
	}
}

// Delivers reports whether this customer's machines get the rule. The zero
// value does not deliver, so a row read into an unset struct fails closed
// rather than being mistaken for the shipped default.
func (r Rollout) Delivers() bool { return r.Enabled && !r.Kill }

// ValidateRollout bounds the state before it is stored.
func ValidateRollout(r Rollout) error {
	if r.RuleID == "" {
		return fmt.Errorf("%w: rule id is required", ErrInvalidRollout)
	}
	if r.OrganizationID == uuid.Nil {
		return fmt.Errorf("%w: organization is required", ErrInvalidRollout)
	}
	if r.RolloutPercent < 0 || r.RolloutPercent > 100 {
		return fmt.Errorf("%w: rollout percent %d is not between 0 and 100", ErrInvalidRollout, r.RolloutPercent)
	}
	if len(r.CanaryGroup) > maxCanaryGroupLen {
		return fmt.Errorf("%w: canary group is longer than %d characters", ErrInvalidRollout, maxCanaryGroupLen)
	}
	return nil
}
