package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A rule's rollout state, against a real database.

func TestStoreRolloutRoundTripsAndDefaultsWhenAbsent(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	// A customer who has never touched the rule gets the shipped default.
	got := mustListRollouts(t, s, e.ctx, e.org)
	assert.Empty(t, got)
	assert.True(t, RolloutFor(got, e.org, "disk-critical").Delivers(),
		"a rule with no row is not configured, not switched off")

	want := DefaultRollout(e.org, "disk-critical")
	want.Kill = true
	want.CanaryGroup = "pilot"
	want.RolloutPercent = 25
	want.UpdatedBy = "ivan"
	require.NoError(t, s.UpsertRollout(e.ctx, want))

	got = mustListRollouts(t, s, e.ctx, e.org)
	require.Len(t, got, 1)
	stored := RolloutFor(got, e.org, "disk-critical")
	assert.True(t, stored.Kill)
	assert.False(t, stored.Delivers(), "a killed rule stops being delivered")
	assert.Equal(t, "pilot", stored.CanaryGroup)
	assert.Equal(t, 25, stored.RolloutPercent)
	assert.False(t, stored.StageEnteredAt.IsZero())
}

func TestStoreRefusesRolloutOutsideItsBounds(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bad := DefaultRollout(e.org, "disk-critical")
	bad.RolloutPercent = 101
	require.ErrorIs(t, s.UpsertRollout(e.ctx, bad), ErrInvalidRollout)

	require.ErrorIs(t, s.SetRolloutStage(e.ctx, e.org, "disk-critical", 101, "rollout"), ErrInvalidRollout)
}

// Moving a rule to another stage restamps the clock the next hold is measured
// from. Leaving the original stamp would let a rule that has just been reverted
// count the hours it spent on the stage it failed, and advance straight back
// into it.
func TestStoreStageChangeRestampsTheHoldClock(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	// A customer with no row is at full reach. Staging the rule writes the row.
	require.NoError(t, s.SetRolloutStage(e.ctx, e.org, "disk-critical", PercentFor(StageCanary), "rollout"))
	canary := RolloutFor(mustListRollouts(t, s, e.ctx, e.org), e.org, "disk-critical")
	assert.Equal(t, PercentFor(StageCanary), canary.RolloutPercent)
	assert.True(t, canary.Delivers(), "staging a rule does not switch it off")
	require.False(t, canary.StageEnteredAt.IsZero())

	require.NoError(t, s.SetRolloutStage(e.ctx, e.org, "disk-critical", PercentFor(StageStaged), "rollout"))
	advanced := RolloutFor(mustListRollouts(t, s, e.ctx, e.org), e.org, "disk-critical")
	assert.Equal(t, PercentFor(StageStaged), advanced.RolloutPercent)
	assert.True(t, advanced.StageEnteredAt.After(canary.StageEnteredAt),
		"the new stage is held from the moment it was entered")
}

// A stage change is the rollout machinery moving a rule, and it must not undo a
// stop somebody reached for. A killed rule that reverted a stage stays killed.
func TestStoreStageChangeLeavesAKillAlone(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	killed := DefaultRollout(e.org, "disk-critical")
	killed.Kill = true
	killed.CanaryGroup = "pilot"
	killed.UpdatedBy = "ivan"
	require.NoError(t, s.UpsertRollout(e.ctx, killed))

	require.NoError(t, s.SetRolloutStage(e.ctx, e.org, "disk-critical", PercentFor(StageCanary), "rollout"))
	got := RolloutFor(mustListRollouts(t, s, e.ctx, e.org), e.org, "disk-critical")
	assert.True(t, got.Kill, "the rollout machinery must not resurrect a killed rule")
	assert.False(t, got.Delivers())
	assert.Equal(t, "pilot", got.CanaryGroup, "and must not overwrite what the customer set")
	assert.Equal(t, PercentFor(StageCanary), got.RolloutPercent)
}

// The durable third of coverage. A machine that cannot evaluate a rule stays
// counted while it is offline and across a restart, because that is a standing
// fact about the estate rather than a liveness reading.
