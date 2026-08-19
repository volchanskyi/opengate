package rules

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// What a rule has to earn before it reaches more of an estate.
//
// A stage is held for a minimum, and the minimum is not what moves the rule —
// what moves it is the estate having stayed quiet while it was held. Advancing
// on a timer alone would give a bad rule an hour's patience and then the whole
// fleet; advancing on evidence gives it an hour's patience and then a smaller
// population than it started with.
//
// Nothing here reads a counter itself. The signals arrive through a port, so the
// machinery is decided and proven against stated inputs rather than against
// whatever a live estate happens to be doing at the time.

// GateReport is what a rule did to one customer's machines over a span. Every
// field counts occurrences, so zero is the only clean answer and a partial read
// cannot be mistaken for a quiet one.
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

// Clean reports whether every signal is quiet. A gate is clean only when all of
// them are: reading one and ignoring the others looks like protection while
// offering a third of it.
func (r GateReport) Clean() bool {
	return r.CeilingBreaches == 0 && r.ThrottleTrips == 0 && r.EvaluationErrors == 0
}

// GateReporter answers what one rule has done to one customer's machines since a
// moment. It is a port rather than a read of a counter here, because the
// counters it reads are raised elsewhere — the alert ceiling by the ingest path,
// the throttle by the agents themselves — and a stage machine that guessed at
// them would be worse than none: it would look like protection.
type GateReporter interface {
	// RuleGate reports what happened since `since`. An error means the question
	// could not be answered, which a caller must read as "not proven quiet"
	// rather than as a clean gate.
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
	// StageHalt is a tripped gate on a rollout that has nowhere left to fall
	// back to. The rule stays on its smallest population rather than being
	// pulled off the machines that are watching it; stopping it altogether is a
	// kill, which is somebody's decision rather than a timer's.
	StageHalt StageAction = "halt"
)

// StageDecision is one evaluation's conclusion: what to do, and the stage and
// reach the rollout has afterwards.
type StageDecision struct {
	Action  StageAction
	Stage   Stage
	Percent int
}

// DecideStage works out what happens to one rollout now.
//
// A tripped gate always wins over an elapsed hold, so a rule that has been
// misbehaving for six hours reverts rather than advancing on the strength of
// having survived them.
func DecideStage(r Rollout, report GateReport, now time.Time) StageDecision {
	stage := r.Stage()
	hold := StageDecision{Action: StageHold, Stage: stage, Percent: r.RolloutPercent}

	// A rule somebody stopped is not rolling out. Walking a killed rule forward
	// on a timer would re-deliver exactly what they reached for the switch to
	// stop.
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

	// A row whose stage clock was never stamped has not held for anything. It is
	// not a rollout that has been quiet since the beginning of time.
	if r.StageEnteredAt.IsZero() || now.Sub(r.StageEnteredAt) < r.HoldFor(stage) {
		return hold
	}

	forward := nextStage(stage)
	if forward == stage {
		return hold
	}
	return StageDecision{Action: StageAdvance, Stage: forward, Percent: r.PercentForStage(forward)}
}

// Apply returns the rollout state to store. A move stamps the moment the new
// stage was entered, because that is what the next hold is measured from — and
// it is also what stops a signal that comes and goes from ratcheting a rule back
// up, since the stage it fell back to has to be earned again from here.
func (d StageDecision) Apply(r Rollout, now time.Time) Rollout {
	if d.Action == StageHold || d.Action == StageHalt {
		return r
	}
	r.RolloutPercent = d.Percent
	r.StageEnteredAt = now
	return r
}

// nextStage is the stage after this one, or the stage itself at the end of the
// road.
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
