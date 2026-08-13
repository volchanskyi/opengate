package rules

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A customer who has never touched a rule has no row for it. That is not the
// same as having switched it off, and reading it that way would leave a fresh
// customer silently unmonitored — so the default is stated here and tested,
// rather than falling out of Go's zero value.
func TestDefaultRolloutDeliversTheRule(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	got := DefaultRollout(org, "disk-critical")

	assert.True(t, got.Enabled, "a rule nobody has configured is on")
	assert.False(t, got.Kill)
	assert.Equal(t, 100, got.RolloutPercent, "a rule nobody has staged reaches the whole estate")
	assert.True(t, got.Delivers())
	assert.Equal(t, org, got.OrganizationID)
	assert.Equal(t, "disk-critical", got.RuleID)
}

// The zero value is what a mistaken read of a missing row would produce. It must
// not deliver, so that mistake is loud rather than silent.
func TestZeroRolloutDoesNotDeliver(t *testing.T) {
	t.Parallel()

	assert.False(t, Rollout{}.Delivers(),
		"an unset rollout must never be mistaken for the shipped default")
}

func TestRolloutStopsDelivery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Rollout)
		want    bool
		because string
	}{
		{
			name:    "switched off",
			mutate:  func(r *Rollout) { r.Enabled = false },
			want:    false,
			because: "a customer who turned the rule off gets no rule",
		},
		{
			name:    "killed",
			mutate:  func(r *Rollout) { r.Kill = true },
			want:    false,
			because: "a kill stops the rule without waiting for a deploy",
		},
		{
			// A kill is not undone by the rule still being enabled: it is the
			// stop, and stopping wins.
			name:    "killed while enabled",
			mutate:  func(r *Rollout) { r.Enabled, r.Kill = true, true },
			want:    false,
			because: "a kill outranks the rule being on",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := DefaultRollout(uuid.New(), "disk-critical")
			tc.mutate(&r)
			assert.Equal(t, tc.want, r.Delivers(), tc.because)
		})
	}
}

func TestValidateRolloutBoundsItsStoredState(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	require.NoError(t, ValidateRollout(DefaultRollout(org, "disk-critical")))

	for _, percent := range []int{-1, 101} {
		r := DefaultRollout(org, "disk-critical")
		r.RolloutPercent = percent
		assert.ErrorIs(t, ValidateRollout(r), ErrInvalidRollout)
	}

	empty := DefaultRollout(org, "")
	assert.ErrorIs(t, ValidateRollout(empty), ErrInvalidRollout)

	long := DefaultRollout(org, "disk-critical")
	long.CanaryGroup = string(make([]byte, maxCanaryGroupLen+1))
	assert.ErrorIs(t, ValidateRollout(long), ErrInvalidRollout)
}
