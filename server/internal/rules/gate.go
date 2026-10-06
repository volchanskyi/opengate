package rules

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// GateReport is what a rule did to one customer's machines over a span.
// Every field counts occurrences, so zero is the only clean answer.
type GateReport struct {
	// CeilingBreaches counts the times this rule pushed the customer past the
	// alert ceiling they are allowed to raise.
	CeilingBreaches int
	// ThrottleTrips counts machines that stopped evaluating the rule because it
	// cost more than its allowance there.
	ThrottleTrips int
	// EvaluationErrors counts the times the rule could not be evaluated at all.
	EvaluationErrors int
}

// Clean reports whether every signal is quiet; a gate is clean only when all three are.
func (r GateReport) Clean() bool {
	return r.CeilingBreaches == 0 && r.ThrottleTrips == 0 && r.EvaluationErrors == 0
}

// GateReporter answers what one rule has done to one customer's machines since a moment.
// The counters behind it are raised elsewhere: the ceiling by ingest, the throttle by agents.
type GateReporter interface {
	// RuleGate reports what happened since `since`; an error means "not proven quiet".
	RuleGate(ctx context.Context, organizationID uuid.UUID, ruleID string, since time.Time) (GateReport, error)
}

// StageAction is what a rollout evaluation concluded.
type StageAction string

const (
	// StageHold leaves the rollout where it is.
	StageHold StageAction = "hold"
	// StageAdvance moves it to the next stage.
	StageAdvance StageAction = "advance"
	// StageRevert moves it back to the stage before.
	StageRevert StageAction = "revert"
	// StageHalt is a tripped gate on a rollout with no earlier stage; the rule stays on its
	// smallest population, and stopping it is a kill, which an operator decides.
	StageHalt StageAction = "halt"
)

// StageDecision is one evaluation's conclusion: what to do, and the stage and
// reach the rollout has afterwards.
type StageDecision struct {
	Action  StageAction
	Stage   Stage
	Percent int
}

// DecideStage works out what happens to one rollout now; a tripped gate wins over an elapsed hold.
func DecideStage(r Rollout, report GateReport, now time.Time) StageDecision {
	stage := r.Stage()
	hold := StageDecision{Action: StageHold, Stage: stage, Percent: r.RolloutPercent}

	// A rule that is killed, disabled or off stays where it is, so a timer never advances it.
	if !r.Delivers() || stage == StageOff {
		return hold
	}

	if !report.Clean() {
		back := previousStage(stage)
		if back == stage {
			return StageDecision{Action: StageHalt, Stage: stage, Percent: r.RolloutPercent}
		}
		return StageDecision{Action: StageRevert, Stage: back, Percent: r.PercentForStage(back)}
	}

	// A row with an unstamped stage clock has held for nothing and cannot advance.
	if r.StageEnteredAt.IsZero() || now.Sub(r.StageEnteredAt) < r.HoldFor(stage) {
		return hold
	}

	forward := nextStage(stage)
	if forward == stage {
		return hold
	}
	return StageDecision{Action: StageAdvance, Stage: forward, Percent: r.PercentForStage(forward)}
}

// Apply returns the rollout state to store; a move stamps when the new stage was entered,
// which starts the next hold.
func (d StageDecision) Apply(r Rollout, now time.Time) Rollout {
	if d.Action == StageHold || d.Action == StageHalt {
		return r
	}
	r.RolloutPercent = d.Percent
	r.StageEnteredAt = now
	return r
}

// nextStage is the stage after this one, or the stage itself at the end.
func nextStage(stage Stage) Stage {
	switch stage {
	case StageCanary:
		return StageStaged
	case StageStaged:
		return StageFull
	case StageOff, StageFull:
		return stage
	default:
		return stage
	}
}

// previousStage is the stage before this one, or the stage itself when there is
// nothing smaller to fall back to.
func previousStage(stage Stage) Stage {
	switch stage {
	case StageFull:
		return StageStaged
	case StageStaged:
		return StageCanary
	case StageOff, StageCanary:
		return stage
	default:
		return stage
	}
}
