package rules

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const maxCanaryGroupLen = 64

// The default pace of a rule, and the bounds an operator may move it inside.
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
	// CanaryGroup is a name stored with the rollout; InStage decides which machines are in a stage.
	CanaryGroup string
	// RolloutPercent is how much of the estate the rule has reached, 0 to 100.
	RolloutPercent int
	// Kill stops the rule for the whole customer, separate from the customer's own Enabled choice.
	Kill           bool
	StageEnteredAt time.Time

	// The populations each partial stage reaches and how long each is held, set per customer.
	CanaryPercent int
	StagedPercent int
	CanaryHold    time.Duration
	StagedHold    time.Duration

	UpdatedAt time.Time
	UpdatedBy string
}

// DefaultRollout is what applies to a customer with no stored row: enabled and fully reached.
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

// Stage is how far along this rollout is, read against the populations the customer set.
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

// paced fills in the shipped pace for each field that is exactly zero.
// A negative field is kept so validation can refuse it.
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

// Delivers reports whether this customer's machines get the rule; the zero value fails closed.
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

// validatePace bounds the populations and holds, and requires the canary to be smaller than staged.
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
