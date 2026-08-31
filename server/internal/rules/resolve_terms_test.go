package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// A rule that names extra conditions has to put every one of them on the wire
// with its own numbers. Contoso's "disk filling on a busy server" rule fires
// only when a full disk and a loaded CPU are true at the same instant, so the
// term carries a comparator, a threshold and a clear of its own — none of them
// the ones the rule itself states. Dropping a term, or carrying the rule's
// numbers into it, turns a two-condition rule into a one-condition rule that
// pages on a disk nobody is using.
func TestResolveCarriesEveryTermOntoTheWire(t *testing.T) {
	t.Parallel()

	c := newContoso()
	def := diskCritical(t)
	// Deliberately unlike the rule's own gte/90/85, so a term that inherited
	// the rule's numbers instead of keeping its own fails here.
	def.All = []Term{{
		Metric:         "cpu.used_percent",
		ComparatorName: "lt",
		Threshold:      20,
		Clear:          25,
		PredicateName:  "WindowMean",
		WindowSecs:     120,
	}}

	rule := Resolve(def, c.fs01, nil)

	require.Len(t, rule.All, 1)
	term := rule.All[0]
	assert.Equal(t, "cpu.used_percent", term.Metric)
	assert.Equal(t, protocol.AlertComparatorLt, term.Comparator)
	assert.InDelta(t, 20.0, term.Threshold, 0)
	assert.InDelta(t, 25.0, term.Clear, 0)
	assert.Equal(t, protocol.RulePredicateWindowMean, term.Predicate)
	assert.Equal(t, uint32(120), term.WindowSecs)

	// The rule's own numbers are untouched by the term's.
	assert.Equal(t, protocol.AlertComparatorGte, rule.Comparator)
	assert.InDelta(t, 90.0, rule.Threshold, 0)
}

// A term may be written against a name from before the vitals rename, the same
// as the rule itself may. It resolves to the dimension the fleet collects, or
// carries through untouched when the catalogue has never heard of it — either
// way the term reaches the machine rather than being dropped.
func TestResolveCanonicalisesATermsMetric(t *testing.T) {
	t.Parallel()

	c := newContoso()
	def := diskCritical(t)
	def.All = []Term{
		{Metric: "mem.used", ComparatorName: "gt", Threshold: 80},
		{Metric: "vendor.widget.depth", ComparatorName: "gt", Threshold: 1},
	}

	rule := Resolve(def, c.fs01, nil)

	require.Len(t, rule.All, 2)
	assert.Equal(t, "mem.used_percent", rule.All[0].Metric, "an alias resolves to the canonical name")
	assert.Equal(t, "vendor.widget.depth", rule.All[1].Metric, "an unknown name carries through")
}

// A rule stating no extra conditions puts nothing on the wire, rather than an
// empty list the agent would have to distinguish from absence.
func TestResolveSendsNoTermsWhenTheRuleStatesNone(t *testing.T) {
	t.Parallel()

	c := newContoso()
	assert.Nil(t, Resolve(diskCritical(t), c.fs01, nil).All)
}
