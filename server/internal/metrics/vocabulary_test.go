// This external test package imports the domain packages that produce the label values,
// which themselves import metrics.
package metrics_test

import (
	"sort"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

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

func TestSuppressionReasonsAreExportedOutcomes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "organization_ceiling", string(alerts.CeilingSuppressed),
		"the suppression reason label is the outcome the store reports")
}

func TestEveryAlertSuppressionReasonStartsAtZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{string(alerts.CeilingSuppressed)}, metrics.AlertSuppressionReasons(),
		"the published reasons are the store's own suppression outcome")
	m := metrics.NewMetrics(prometheus.NewRegistry())
	require.Equal(t, len(metrics.AlertSuppressionReasons()), testutil.CollectAndCount(m.AlertsSuppressedTotal),
		"every reason is published before any alert is refused")
}

func TestNoShippedRuleClaimsTheCatchAllLabel(t *testing.T) {
	t.Parallel()

	catalogue, err := rules.Embedded()
	require.NoError(t, err)
	for _, def := range catalogue.All() {
		require.NotEqualf(t, metrics.UnknownRule, def.ID,
			"%s collides with the label reserved for rules this build does not ship", def.ID)
	}
}
