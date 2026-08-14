package rules

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Falling back, and what a rollout looks like afterwards.

// A rule already on its smallest population has nowhere to fall back to, so it
// stops there rather than being pulled off the machines that are watching it —
// and asking again gives the same answer, so a repeated evaluation cannot walk it
// down.
func TestRevertIsBoundedAtTheCanaryAndIdempotent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tripped := GateReport{ThrottleTrips: 1}
	canary := staged(StageCanary, 2*time.Hour, now)

	got := DecideStage(canary, tripped, now)
	assert.Equal(t, StageHalt, got.Action, "there is no stage below the canary")
	assert.Equal(t, StageCanary, got.Stage)
	assert.Equal(t, canary.RolloutPercent, got.Percent, "a halt changes nothing about the reach")

	// Applying a halt and deciding again lands in the same place.
	again := DecideStage(got.Apply(canary, now), tripped, now)
	assert.Equal(t, got, again, "deciding twice must not walk a rule further down")
}

// A signal that comes and goes cannot ratchet a rule back up: reverting restarts
// the hold, so the stage it fell back to has to be earned again from the moment
// it fell back.
func TestAFlappingSignalCannotWalkAStageUpAndDown(t *testing.T) {
	t.Parallel()

	start := time.Now()
	full := staged(StageFull, 0, start.Add(-24*time.Hour))

	reverted := DecideStage(full, GateReport{CeilingBreaches: 1}, start)
	require.Equal(t, StageRevert, reverted.Action)
	require.Equal(t, StageStaged, reverted.Stage)

	after := reverted.Apply(full, start)
	assert.Equal(t, start, after.StageEnteredAt, "a revert restarts the hold")

	// The signal clears immediately. The stage it fell back to still has to be
	// held for its own minimum before it may move again.
	assert.Equal(t, StageHold,
		DecideStage(after, GateReport{}, start.Add(time.Minute)).Action,
		"a signal that cleared does not hand back the stage it cost")
	assert.Equal(t, StageAdvance,
		DecideStage(after, GateReport{}, start.Add(6*time.Hour)).Action,
		"and the stage is earned again by holding it")
}

// A rule that has reached the whole estate has nowhere to advance to, and a rule
// that reaches nobody is not rolling out at all.
func TestNothingToAdvanceIsAHold(t *testing.T) {
	t.Parallel()

	now := time.Now()
	assert.Equal(t, StageHold, decide(StageFull, 30*24*time.Hour, GateReport{}, now).Action,
		"the whole estate is the end of the road")

	off := staged(StageCanary, 24*time.Hour, now)
	off.RolloutPercent = 0
	assert.Equal(t, StageHold, DecideStage(off, GateReport{}, now).Action)
}

// A stopped rule never advances. A kill is somebody intervening on a rule that
// is degrading machines, and a rollout that kept walking it forward on a timer
// would be re-delivering exactly what they stopped.
func TestAStoppedRuleNeverAdvances(t *testing.T) {
	t.Parallel()

	now := time.Now()
	for _, tc := range []struct {
		name   string
		mutate func(*Rollout)
	}{
		{"killed", func(r *Rollout) { r.Kill = true }},
		{"switched off", func(r *Rollout) { r.Enabled = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := staged(StageCanary, 24*time.Hour, now)
			tc.mutate(&r)
			assert.Equal(t, StageHold, DecideStage(r, GateReport{}, now).Action)
		})
	}
}

// A row whose stage clock was never stamped is not one whose hold has elapsed
// since the beginning of time. Reading it that way would advance a rollout on
// its first evaluation.
func TestAnUnstampedStageClockHolds(t *testing.T) {
	t.Parallel()

	now := time.Now()
	r := staged(StageCanary, 0, now)
	r.StageEnteredAt = time.Time{}
	assert.Equal(t, StageHold, DecideStage(r, GateReport{}, now).Action)
}

// Applying a decision produces the row to store: the new reach, stamped with the
// moment the stage was entered, because the hold is measured from it.
func TestApplyStampsTheMomentTheStageWasEntered(t *testing.T) {
	t.Parallel()

	now := time.Now()
	canary := staged(StageCanary, time.Hour, now)

	advanced := DecideStage(canary, GateReport{}, now).Apply(canary, now)
	assert.Equal(t, PercentFor(StageStaged), advanced.RolloutPercent)
	assert.Equal(t, now, advanced.StageEnteredAt)
	assert.Equal(t, canary.OrganizationID, advanced.OrganizationID)
	assert.Equal(t, canary.RuleID, advanced.RuleID)

	held := DecideStage(staged(StageCanary, time.Minute, now), GateReport{}, now)
	unchanged := held.Apply(canary, now)
	assert.Equal(t, canary, unchanged, "a hold leaves the row exactly as it was")
}

// A gate is clean only when every signal is. Reading one of the three and
// ignoring the others would look like protection while offering two thirds less.
func TestAGateIsCleanOnlyWhenEverySignalIs(t *testing.T) {
	t.Parallel()

	assert.True(t, GateReport{}.Clean())
	assert.False(t, GateReport{CeilingBreaches: 1}.Clean())
	assert.False(t, GateReport{ThrottleTrips: 1}.Clean())
	assert.False(t, GateReport{EvaluationErrors: 1}.Clean())
}
