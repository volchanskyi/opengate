package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cost of a rule is the readings it retains and may touch, which is the
// number the agent's own evaluator charges. These cases pin that the two agree:
// an instant reading costs one, a windowed one costs its window plus the second
// that closes it, and a conjunction costs every side it requires.
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
			// An empty predicate is the plain threshold an older rule states by
			// saying nothing, and it costs what Instant costs.
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

// Cost must be monotone in the window, or a budget cannot bound anything: a
// wider window that charged less would let an unbounded rule through.
func TestRuleCostIsMonotoneInTheWindow(t *testing.T) {
	t.Parallel()

	var prev uint64
	for _, window := range []uint32{0, 1, 30, 300, 3600} {
		cost := RuleCost(Definition{PredicateName: "WindowMean", WindowSecs: window})
		assert.GreaterOrEqual(t, cost, prev, "cost fell as the window grew")
		prev = cost
	}
}

// The CI cost gate. A rule that would make an endpoint hold more readings than
// the per-agent budget allows must fail the build here, on the machine that is
// free, rather than on five thousand endpoints that are not. A gate that has
// never been seen to fail is not a gate, so this proves it fires.
func TestLoadCatalogueRejectsARuleOverThePerRuleBudget(t *testing.T) {
	t.Parallel()

	overBudget := strings.ReplaceAll(validYAML,
		"    predicate: Instant\n",
		fmt.Sprintf("    predicate: WindowMean\n    window_secs: %d\n", MaxRuleCost))

	_, err := loadFixture(t, overBudget)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cost")

	// The same rule just inside the budget loads, so the gate is a boundary and
	// not a blanket refusal of windowed rules.
	withinBudget := strings.ReplaceAll(validYAML,
		"    predicate: Instant\n",
		fmt.Sprintf("    predicate: WindowMean\n    window_secs: %d\n", MaxRuleCost-1))
	_, err = loadFixture(t, withinBudget)
	require.NoError(t, err)
}

// The per-rule budget alone does not bound an endpoint: a hundred rules each
// just inside it would still sink the agent, so the catalogue's total is
// bounded too.
func TestLoadCatalogueRejectsACatalogueOverTheFleetBudget(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("rules:\n")
	// Each rule is at the per-rule ceiling, so enough of them cross the total.
	count := int(MaxCatalogueCost/(MaxRuleCost-1)) + 2
	for i := range count {
		fmt.Fprintf(&b, `  - id: filler-%d
    version: 1
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

// The shipped catalogue must itself be inside the budget it enforces.
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
