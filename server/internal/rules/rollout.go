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

// The pace a rule spreads at when nobody has said otherwise, and the range an
// operator may move it inside.
//
// A percentage is a share of an estate, so it is bounded away from both ends: a
// stage reaching nobody is not a stage, and a stage reaching everybody is not a
// stage either — it is the rollout finishing before anything was learned. The
// holds are bounded the same way: a stage held for seconds proves nothing, and
// one held for a year is a rule that never arrives.
const (
	defaultCanaryPercent = 1
	defaultStagedPercent = 10
	defaultCanaryHold    = time.Hour
	defaultStagedHold    = 6 * time.Hour

	minStagePercent = 1
	maxStagePercent = 99
	minStageHold    = time.Minute
	maxStageHold    = 30 * 24 * time.Hour
)

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

	// The populations each partial stage reaches, and how long each is held
	// before it may advance. They are the customer's, because an estate of
	// twelve machines and an estate of five thousand do not want the same first
	// stage, and an hour is the wrong hold for a rule whose symptom takes a
	// working day to appear.
	//
	// There is deliberately nothing here for switching the automatic pull-back
	// off. It is the mitigation for the one thing in this program that can
	// degrade an estate at once, so it is not configuration — and a field that
	// does not exist is a field no API can expose.
	CanaryPercent int
	StagedPercent int
	CanaryHold    time.Duration
	StagedHold    time.Duration

	UpdatedAt time.Time
	UpdatedBy string
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
		CanaryPercent:  defaultCanaryPercent,
		StagedPercent:  defaultStagedPercent,
		CanaryHold:     defaultCanaryHold,
		StagedHold:     defaultStagedHold,
	}
}

// Stage is how far along this rollout is, read against the populations the
// customer set. Classifying a quarter of an estate as a canary because the code
// ships a tenth would hold a rule at a stage it has already left.
func (r Rollout) Stage() Stage {
	paced := r.paced()
	switch {
	case r.RolloutPercent <= 0:
		return StageOff
	case r.RolloutPercent < paced.StagedPercent:
		return StageCanary
	case r.RolloutPercent < fullPercent:
		return StageStaged
	default:
		return StageFull
	}
}

// PercentForStage is the reach this rollout's stage rolls to.
func (r Rollout) PercentForStage(stage Stage) int {
	paced := r.paced()
	switch stage {
	case StageCanary:
		return paced.CanaryPercent
	case StageStaged:
		return paced.StagedPercent
	case StageFull:
		return fullPercent
	case StageOff:
		return 0
	default:
		return 0
	}
}

// HoldFor is the minimum this rollout holds a stage for.
func (r Rollout) HoldFor(stage Stage) time.Duration {
	paced := r.paced()
	switch stage {
	case StageCanary:
		return paced.CanaryHold
	case StageStaged:
		return paced.StagedHold
	case StageOff, StageFull:
		return 0
	default:
		return 0
	}
}

// paced fills in the shipped pace for anything left unset, so a rollout built
// from a partial struct behaves like a customer who has configured nothing.
//
// Unset is exactly zero. A negative is not an absent value, it is a stated one
// that makes no sense, and defaulting it away here would leave validation with
// nothing to refuse.
func (r Rollout) paced() Rollout {
	if r.CanaryPercent == 0 {
		r.CanaryPercent = defaultCanaryPercent
	}
	if r.StagedPercent == 0 {
		r.StagedPercent = defaultStagedPercent
	}
	if r.CanaryHold == 0 {
		r.CanaryHold = defaultCanaryHold
	}
	if r.StagedHold == 0 {
		r.StagedHold = defaultStagedHold
	}
	return r
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
	return validatePace(r)
}

// validatePace bounds the populations and the waiting periods. A stage has to
// reach somebody and stop short of everybody, and the canary has to be smaller
// than the stage after it — a rollout that shrank on its way forward would pull
// a rule off the machines already proving it.
func validatePace(r Rollout) error {
	paced := r.paced()
	for _, stage := range []struct {
		what    string
		percent int
	}{
		{"canary", paced.CanaryPercent},
		{"staged", paced.StagedPercent},
	} {
		if stage.percent < minStagePercent || stage.percent > maxStagePercent {
			return fmt.Errorf("%w: the %s population is %d%%, outside %d–%d%%",
				ErrInvalidRollout, stage.what, stage.percent, minStagePercent, maxStagePercent)
		}
	}
	if paced.CanaryPercent >= paced.StagedPercent {
		return fmt.Errorf("%w: the canary population (%d%%) must be smaller than the staged one (%d%%)",
			ErrInvalidRollout, paced.CanaryPercent, paced.StagedPercent)
	}

	for _, stage := range []struct {
		what string
		hold time.Duration
	}{
		{"canary", paced.CanaryHold},
		{"staged", paced.StagedHold},
	} {
		if stage.hold < minStageHold || stage.hold > maxStageHold {
			return fmt.Errorf("%w: the %s waiting period is %s, outside %s–%s",
				ErrInvalidRollout, stage.what, stage.hold, minStageHold, maxStageHold)
		}
	}
	return nil
}
