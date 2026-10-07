package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleCostMatchesTheAgentsCharge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		def  Definition
		want uint64
	}{
		{
			name: "instant reading costs one sample",
			def:  Definition{PredicateName: "Instant"},
			want: 1,
		},
		{
			name: "a window holds every second plus the one that closes it",
			def:  Definition{PredicateName: "WindowMax", WindowSecs: 300},
			want: 301,
		},
		{
			name: "a rate holds both ends of its window",
			def:  Definition{PredicateName: "Rate", WindowSecs: 60},
			want: 61,
		},
		{
			name: "an unstated predicate is an instant reading",
			def:  Definition{},
			want: 1,
		},
		{
			name: "a conjunction costs its own condition plus every extra one",
			def: Definition{
				PredicateName: "Instant",
				All: []Term{
					{PredicateName: "WindowMean", WindowSecs: 120},
					{PredicateName: "Instant"},
				},
			},
			want: 1 + 121 + 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, RuleCost(tc.def))
		})
	}
}

func TestRuleCostIsMonotoneInTheWindow(t *testing.T) {
	t.Parallel()

	var prev uint64
	for _, window := range []uint32{0, 1, 30, 300, 3600} {
		cost := RuleCost(Definition{PredicateName: "WindowMean", WindowSecs: window})
		assert.GreaterOrEqual(t, cost, prev, "cost fell as the window grew")
		prev = cost
	}
}

func TestLoadCatalogueRejectsARuleOverThePerRuleBudget(t *testing.T) {
	t.Parallel()

	overBudget := strings.ReplaceAll(validYAML,
		"    predicate: Instant\n",
		fmt.Sprintf("    predicate: WindowMean\n    window_secs: %d\n", MaxRuleCost))

	_, err := loadFixture(t, overBudget)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cost")

	withinBudget := strings.ReplaceAll(validYAML,
		"    predicate: Instant\n",
		fmt.Sprintf("    predicate: WindowMean\n    window_secs: %d\n", MaxRuleCost-1))
	_, err = loadFixture(t, withinBudget)
	require.NoError(t, err)
}

func TestLoadCatalogueRejectsACatalogueOverTheFleetBudget(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("rules:\n")
	count := int(MaxCatalogueCost/(MaxRuleCost-1)) + 2
	for i := range count {
		fmt.Fprintf(&b, `  - id: filler-%d
    version: 1
    severity: warning
    summary: Fills the budget.
    metric: cpu.total
    comparator: gte
    threshold: 90
    clear: 80
    sustain_secs: 60
    predicate: WindowMean
    window_secs: %d
    group_by: [device]
    group_window_secs: 300
`, i, MaxRuleCost-2)
	}

	_, err := loadFixture(t, b.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "budget")
}

func TestEmbeddedCatalogueIsWithinTheFleetBudget(t *testing.T) {
	t.Parallel()

	cat, err := Embedded()
	require.NoError(t, err)

	var total uint64
	for _, def := range cat.All() {
		cost := RuleCost(def)
		assert.LessOrEqualf(t, cost, MaxRuleCost, "%s costs %d readings", def.ID, cost)
		total += cost
	}
	assert.LessOrEqual(t, total, MaxCatalogueCost,
		"the shipped catalogue asks every endpoint to hold %d readings", total)
}
