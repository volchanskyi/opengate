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
}

// The durable third of coverage. A machine that cannot evaluate a rule stays
// counted while it is offline and across a restart, because that is a standing
// fact about the estate rather than a liveness reading.
