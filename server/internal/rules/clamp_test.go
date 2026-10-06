package rules

import (
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/settings"
)

func TestANarrowedRangeMovesAValueToTheNearestAllowed(t *testing.T) {
	t.Parallel()

	narrowed := narrowedDisk(t, Bounds{Min: 50, Max: 90})
	org := uuid.New()
	tuned := orgBinding(org, narrowed.ID, threshold(95))

	clamped, moves := ClampBinding(narrowed, tuned)

	assert.InEpsilon(t, 90.0, clamped.Params["threshold"], 0.0001,
		"the value moves to the nearest allowed, not to what the rule ships")
	require.Len(t, moves, 1)
	assert.Equal(t, "threshold", moves[0].Param)
	assert.Equal(t, tuned.ID, moves[0].BindingID)
	assert.Equal(t, narrowed.Version, moves[0].RuleVersion)
	assert.InEpsilon(t, 95.0, moves[0].From, 0.0001)
	assert.InEpsilon(t, 90.0, moves[0].To, 0.0001)

	assert.InEpsilon(t, 95.0, tuned.Params["threshold"], 0.0001)
}

func TestAValueTheNewVersionStillAllowsIsNotClamped(t *testing.T) {
	t.Parallel()

	narrowed := narrowedDisk(t, Bounds{Min: 50, Max: 90})
	org := uuid.New()

	clamped, moves := ClampBinding(narrowed, orgBinding(org, narrowed.ID, threshold(85)))
	assert.Empty(t, moves)
	assert.InEpsilon(t, 85.0, clamped.Params["threshold"], 0.0001)
}

func TestAValueBelowTheNewFloorMovesUpToIt(t *testing.T) {
	t.Parallel()

	narrowed := narrowedDisk(t, Bounds{Min: 80, Max: 99})
	org := uuid.New()

	clamped, moves := ClampBinding(narrowed, orgBinding(org, narrowed.ID, threshold(60)))
	require.Len(t, moves, 1)
	assert.InEpsilon(t, 80.0, clamped.Params["threshold"], 0.0001)
	assert.InEpsilon(t, 80.0, moves[0].To, 0.0001)
}

func TestAParameterTheNewVersionNoLongerOffersIsRecorded(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	def.Tunable = map[string]Bounds{"threshold": def.Tunable["threshold"]}
	org := uuid.New()

	clamped, moves := ClampBinding(def, orgBinding(org, def.ID,
		map[string]float64{"threshold": 95, "sustain_secs": 600}))

	assert.NotContains(t, clamped.Params, "sustain_secs")
	require.Len(t, moves, 1)
	assert.Equal(t, "sustain_secs", moves[0].Param)
	shipped, _ := def.ShippedParam("sustain_secs")
	assert.InEpsilon(t, shipped, moves[0].To, 0.0001)
}

func TestAClampedRuleKeepsFiringAtTheClampedValue(t *testing.T) {
	t.Parallel()

	// The new ceiling differs from the shipped value, so a clamp and a revert give different numbers.
	narrowed := narrowedDisk(t, Bounds{Min: 50, Max: 92})
	org := uuid.New()
	machine := Device{Scope: settings.Scope{DeviceID: uuid.New(), OrganizationID: org}}

	got := Resolve(narrowed, machine, []Binding{orgBinding(org, narrowed.ID, threshold(95))})

	assert.Equal(t, narrowed.ID, got.ID, "the rule still reaches the machine")
	assert.InEpsilon(t, 92.0, got.Threshold, 0.0001,
		"the wire value is the nearest allowed, not the shipped default")
	assert.NotEqual(t, narrowed.Threshold, got.Threshold,
		"reverting to the shipped value would take the customer's decision away")
}

func TestBoundsNearest(t *testing.T) {
	t.Parallel()

	b := Bounds{Min: 50, Max: 90}
	for _, tc := range []struct {
		name      string
		given     float64
		want      float64
		wantMoved bool
		unchanged bool
	}{
		{name: "above the ceiling", given: 95, want: 90, wantMoved: true},
		{name: "below the floor", given: 10, want: 50, wantMoved: true},
		{name: "on the ceiling", given: 90, want: 90, unchanged: true},
		{name: "on the floor", given: 50, want: 50, unchanged: true},
		{name: "inside", given: 70, want: 70, unchanged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, moved := b.Nearest(tc.given)
			assert.InDelta(t, tc.want, got, 0.0001)
			assert.Equal(t, tc.wantMoved, moved)
			assert.Equal(t, tc.unchanged, !moved)
		})
	}
}

func narrowedDisk(t *testing.T, bounds Bounds) Definition {
	t.Helper()
	def := diskCritical(t)
	def.Version++
	tunable := maps.Clone(def.Tunable)
	tunable["threshold"] = bounds
	def.Tunable = tunable
	return def
}
