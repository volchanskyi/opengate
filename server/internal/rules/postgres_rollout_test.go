package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreRolloutRoundTripsAndDefaultsWhenAbsent(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

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

func TestStoreStageChangeRestampsTheHoldClock(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

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
