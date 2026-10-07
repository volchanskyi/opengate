package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestResolveCarriesEveryTermOntoTheWire(t *testing.T) {
	t.Parallel()

	c := newContoso()
	def := diskCritical(t)
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

	assert.Equal(t, protocol.AlertComparatorGte, rule.Comparator)
	assert.InDelta(t, 90.0, rule.Threshold, 0)
}

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

func TestResolveSendsNoTermsWhenTheRuleStatesNone(t *testing.T) {
	t.Parallel()

	c := newContoso()
	assert.Nil(t, Resolve(diskCritical(t), c.fs01, nil).All)
}
