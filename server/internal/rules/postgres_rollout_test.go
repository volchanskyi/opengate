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
}

func TestStoreRefusesRolloutOutsideItsBounds(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bad := DefaultRollout(e.org, "disk-critical")
	bad.RolloutPercent = 101
	require.ErrorIs(t, s.UpsertRollout(e.ctx, bad), ErrInvalidRollout)
}
