// This file is deliberately the package's external test: it reaches into the
// domain packages that produce the label values, and those packages already
// import the metrics package.
package metrics_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

// The label vocabularies are declared in two places — here, where the series are
// exported, and in the domain, where the values come from. That is a seam, so it
// gets a test: a status added to the incident lifecycle or a state added to
// coverage accounting must not silently stop being exported. A gauge that is
// simply missing a value reads as "no data", which is what nobody notices.

// TestOpenIncidentStatusesAreTheIncidentLifecycleMinusResolved pins the exported
// statuses to the lifecycle itself. A resolved incident is not open, so it is
// not a series on a gauge that says it is.
func TestOpenIncidentStatusesAreTheIncidentLifecycleMinusResolved(t *testing.T) {
	t.Parallel()

	want := make([]string, 0, len(alerts.OpenStatuses()))
	for _, status := range alerts.OpenStatuses() {
		require.NotEqual(t, alerts.StatusResolved, status, "a resolved incident is not open work")
		want = append(want, string(status))
	}

	exported := append([]string{}, metrics.OpenIncidentStatuses()...)
	sort.Strings(want)
	sort.Strings(exported)
	require.Equal(t, want, exported,
		"every status an incident can be open in is exported, and only those")
}

// TestRuleCoverageStatesAreTheWholeFleetSplit pins the exported states to the
// split coverage accounting actually produces. The four together always add up
// to the fleet, so exporting three of them would make a rule look like it was
// watching a smaller estate than it is.
func TestRuleCoverageStatesAreTheWholeFleetSplit(t *testing.T) {
	t.Parallel()

	full := agentapi.RuleCoverageCounts{Active: 1, Throttled: 2, Unsupported: 3, Unknown: 4}
	produced := make([]string, 0, len(full.ByState()))
	for state := range full.ByState() {
		produced = append(produced, state)
	}

	exported := append([]string{}, metrics.RuleCoverageStates()...)
	sort.Strings(produced)
	sort.Strings(exported)
	require.Equal(t, produced, exported,
		"every state a device can be in for a rule is exported, and only those")
}

// TestSuppressionReasonsAreExportedOutcomes keeps the one investigation counter
// that already ships keyed on the store's own vocabulary rather than on a
// spelling repeated by hand at the call site.
func TestSuppressionReasonsAreExportedOutcomes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "organization_ceiling", string(alerts.CeilingSuppressed),
		"the suppression reason label is the outcome the store reports")
}

// TestNoShippedRuleClaimsTheCatchAllLabel keeps the unshipped-rule label a
// signal rather than a collision. It is meant to appear only when something
// reached the counter that should not have, and a rule genuinely named that
// would bury the one inside the other.
func TestNoShippedRuleClaimsTheCatchAllLabel(t *testing.T) {
	t.Parallel()

	catalogue, err := rules.Embedded()
	require.NoError(t, err)
	for _, def := range catalogue.All() {
		require.NotEqualf(t, metrics.UnknownRule, def.ID,
			"%s collides with the label reserved for rules this build does not ship", def.ID)
	}
}
