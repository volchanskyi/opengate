package rules

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// What a rule has to earn before it reaches more of an estate.

// staged builds a rollout sitting in a stage, entered `held` ago.
func staged(stage Stage, held time.Duration, now time.Time) Rollout {
	r := DefaultRollout(uuid.New(), "disk-critical")
	r.RolloutPercent = PercentFor(stage)
	r.StageEnteredAt = now.Add(-held)
	return r
}

// decide evaluates a rollout that has been in `stage` for `held`.
func decide(stage Stage, held time.Duration, report GateReport, now time.Time) StageDecision {
	return DecideStage(staged(stage, held, now), report, now)
}

// A gate that failed is not a rollout that pauses: it is one that goes back to
// the population it was last quiet on. Each signal is its own case, because a
// single combined one passes just as well with two of the three unwired.
func TestATrippedGateRevertsRatherThanAdvances(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name   string
		report GateReport
	}{
		{"the customer's alert ceiling was hit", GateReport{CeilingBreaches: 1}},
		{"a machine throttled the rule", GateReport{ThrottleTrips: 1}},
		{"the rule failed to evaluate", GateReport{EvaluationErrors: 1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Long past the hold: nothing about elapsed time may advance it.
			got := decide(StageStaged, 24*time.Hour, tc.report, now)
			assert.Equal(t, StageRevert, got.Action, "a tripped gate reverts")
			assert.Equal(t, StageCanary, got.Stage, "back to the stage before it")
			assert.Equal(t, PercentFor(StageCanary), got.Percent)
		})
	}
}

// The hold is a minimum, not a trigger. Time alone never moves a rule, and time
// plus a quiet estate moves it exactly one stage — jumping a canary straight to
// the fleet would spend the whole mitigation in one step.
func TestAdvanceNeedsBothTheHoldAndAQuietEstate(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tooSoon := decide(StageCanary, 59*time.Minute, GateReport{}, now)
	assert.Equal(t, StageHold, tooSoon.Action, "the hold has not elapsed")
	assert.Equal(t, StageCanary, tooSoon.Stage)

	elapsed := decide(StageCanary, time.Hour, GateReport{}, now)
	assert.Equal(t, StageAdvance, elapsed.Action)
	assert.Equal(t, StageStaged, elapsed.Stage, "exactly one stage, never two")
	assert.Equal(t, PercentFor(StageStaged), elapsed.Percent)

	longQuiet := decide(StageStaged, 30*24*time.Hour, GateReport{}, now)
	assert.Equal(t, StageAdvance, longQuiet.Action)
	assert.Equal(t, StageFull, longQuiet.Stage, "a month of quiet is still one stage at a time")
}

// Each stage holds for its own minimum. The staged hold is the longer one
// because it is the last stop before the whole estate.
func TestEachStageHoldsForItsOwnMinimum(t *testing.T) {
	t.Parallel()

	now := time.Now()
	assert.Equal(t, StageHold, decide(StageStaged, 5*time.Hour, GateReport{}, now).Action,
		"five hours is not the six a staged rollout holds for")
	assert.Equal(t, StageAdvance, decide(StageStaged, 6*time.Hour, GateReport{}, now).Action)
}
