package metrics

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

var shippedRules = []string{
	"cpu-saturated", "disk-critical", "disk-slow", "io-stalled", "memory-pressure",
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func investigationSeries(t *testing.T, reg *prometheus.Registry) []string {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)

	var series []string
	for _, family := range families {
		if !isInvestigationFamily(family.GetName()) {
			continue
		}
		for _, metric := range family.GetMetric() {
			series = append(series, family.GetName()+labelSetOf(metric))
		}
	}
	sort.Strings(series)
	return series
}

func isInvestigationFamily(name string) bool {
	for _, prefix := range []string{
		"opengate_alerts_", "opengate_incidents_", "opengate_rule_coverage",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func labelSetOf(metric *dto.Metric) string {
	pairs := make([]string, 0, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		pairs = append(pairs, label.GetName()+"="+label.GetValue())
	}
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}

func investigationLabelNames(t *testing.T, reg *prometheus.Registry) []string {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)

	seen := make(map[string]bool)
	for _, family := range families {
		if !isInvestigationFamily(family.GetName()) {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				seen[label.GetName()] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fleetOf(t *testing.T, size int, ruleIDs []string) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(ruleIDs)

	for range size {
		for _, ruleID := range ruleIDs {
			m.ObserveAlertCreated(ruleID)
		}
	}
	// The suppression reason is a closed set, so any fleet size yields the same series.
	m.ObserveAlertSuppressed("organization_ceiling")

	coverage := make(map[string]map[string]int, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		coverage[ruleID] = map[string]int{CoverageActive: size}
	}
	refreshInvestigations(context.Background(), m, InvestigationSource{
		OpenInvestigations: func(context.Context) (map[string]int, int, error) {
			return map[string]int{IncidentNew: size}, size * len(ruleIDs), nil
		},
		FleetRuleCoverage: func(context.Context) (map[string]map[string]int, error) {
			return coverage, nil
		},
	}, discardLogger())
	return reg
}

func TestInvestigationSeriesAreInvariantToFleetSize(t *testing.T) {
	t.Parallel()

	oneMachine := investigationSeries(t, fleetOf(t, 1, shippedRules))
	wholeEstate := investigationSeries(t, fleetOf(t, 5000, shippedRules))

	require.NotEmpty(t, oneMachine, "the investigation series must be exported at all")
	require.Equal(t, oneMachine, wholeEstate,
		"platform meta-monitoring is O(rules); a fleet five thousand times larger exports the same series")
}

func TestInvestigationSeriesGrowOnlyWithTheRuleCount(t *testing.T) {
	t.Parallel()

	fivePack := investigationSeries(t, fleetOf(t, 1, shippedRules))
	sixPack := investigationSeries(t, fleetOf(t, 1, append(append([]string{}, shippedRules...), "net-saturated")))

	// One created-alert counter plus one gauge per coverage state.
	want := len(fivePack) + 1 + len(RuleCoverageStates())
	require.Len(t, sixPack, want,
		"a new rule adds its own series and nothing else")
}

func TestInvestigationSeriesCarryNoPerEntityLabel(t *testing.T) {
	t.Parallel()

	names := investigationLabelNames(t, fleetOf(t, 5000, shippedRules))

	require.Equal(t, []string{"reason", "rule_id", "state", "status"}, names,
		"the investigation series carry only closed vocabularies, never an entity id")
}

func TestInvestigationSeriesCoverTheirClosedVocabularies(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(shippedRules)

	for _, status := range OpenIncidentStatuses() {
		require.InDelta(t, 0, testutil.ToFloat64(m.IncidentsOpen.WithLabelValues(status)), 0,
			"status %s is exported before anything opens in it", status)
	}
	for _, ruleID := range shippedRules {
		require.InDelta(t, 0, testutil.ToFloat64(m.AlertsCreatedTotal.WithLabelValues(ruleID)), 0,
			"rule %s is exported before it ever fires", ruleID)
		for _, state := range RuleCoverageStates() {
			require.InDelta(t, 0, testutil.ToFloat64(m.RuleCoverage.WithLabelValues(ruleID, state)), 0,
				"rule %s state %s is exported before any machine reports it", ruleID, state)
		}
	}

	require.Len(t, investigationSeries(t, reg),
		len(OpenIncidentStatuses())+1+len(AlertSuppressionReasons())+len(shippedRules)*(1+len(RuleCoverageStates())),
		"seeding exports the whole vocabulary and nothing beyond it")
}

func TestOpenGaugesFallBackToZeroWhenTheQueueEmpties(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(shippedRules)

	answer := map[string]int{IncidentNew: 7, IncidentAcknowledged: 2}
	openAlerts := 41
	src := InvestigationSource{
		OpenInvestigations: func(context.Context) (map[string]int, int, error) {
			return answer, openAlerts, nil
		},
		FleetRuleCoverage: func(context.Context) (map[string]map[string]int, error) {
			return nil, nil
		},
	}

	refreshInvestigations(context.Background(), m, src, discardLogger())
	require.InDelta(t, 7, testutil.ToFloat64(m.IncidentsOpen.WithLabelValues(IncidentNew)), 0)
	require.InDelta(t, 2, testutil.ToFloat64(m.IncidentsOpen.WithLabelValues(IncidentAcknowledged)), 0)
	require.InDelta(t, 41, testutil.ToFloat64(m.AlertsOpen), 0)

	answer, openAlerts = map[string]int{}, 0
	refreshInvestigations(context.Background(), m, src, discardLogger())
	for _, status := range OpenIncidentStatuses() {
		require.InDelta(t, 0, testutil.ToFloat64(m.IncidentsOpen.WithLabelValues(status)), 0,
			"an emptied queue reads as zero, never as the last count it held")
	}
	require.InDelta(t, 0, testutil.ToFloat64(m.AlertsOpen), 0)
}

func TestRuleCoverageGaugeClearsARuleThatStopsReporting(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(shippedRules)

	coverage := map[string]map[string]int{
		"disk-critical": {CoverageActive: 40, CoverageUnsupported: 2, CoverageUnknown: 8},
	}
	src := InvestigationSource{
		OpenInvestigations: func(context.Context) (map[string]int, int, error) { return nil, 0, nil },
		FleetRuleCoverage: func(context.Context) (map[string]map[string]int, error) {
			return coverage, nil
		},
	}

	refreshInvestigations(context.Background(), m, src, discardLogger())
	require.InDelta(t, 40, testutil.ToFloat64(m.RuleCoverage.WithLabelValues("disk-critical", CoverageActive)), 0)
	require.InDelta(t, 2, testutil.ToFloat64(m.RuleCoverage.WithLabelValues("disk-critical", CoverageUnsupported)), 0)
	require.InDelta(t, 8, testutil.ToFloat64(m.RuleCoverage.WithLabelValues("disk-critical", CoverageUnknown)), 0)
	require.InDelta(t, 0, testutil.ToFloat64(m.RuleCoverage.WithLabelValues("disk-critical", CoverageThrottled)), 0,
		"a state nothing reported is zero, not absent")

	coverage = map[string]map[string]int{}
	refreshInvestigations(context.Background(), m, src, discardLogger())
	require.InDelta(t, 0, testutil.ToFloat64(m.RuleCoverage.WithLabelValues("disk-critical", CoverageActive)), 0,
		"a rule nothing reports on is watching nothing, and says so")
}

func TestCoverageOfAnUnshippedRuleIsNotExported(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(shippedRules)

	refreshInvestigations(context.Background(), m, InvestigationSource{
		OpenInvestigations: func(context.Context) (map[string]int, int, error) { return nil, 0, nil },
		FleetRuleCoverage: func(context.Context) (map[string]map[string]int, error) {
			return map[string]map[string]int{
				"disk-critical":       {CoverageActive: 3},
				"rule-nobody-ships-🙂": {CoverageActive: 900},
			}, nil
		},
	}, discardLogger())

	for _, series := range investigationSeries(t, reg) {
		require.NotContains(t, series, "rule-nobody-ships",
			"a rule id outside the catalogue cannot mint a coverage series")
	}
}

func TestAlertsCreatedIsBoundedByTheCatalogue(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(shippedRules)

	m.ObserveAlertCreated("disk-critical")
	m.ObserveAlertCreated("../../etc/passwd")
	m.ObserveAlertCreated("rule-from-a-newer-agent")

	require.InDelta(t, 1, testutil.ToFloat64(m.AlertsCreatedTotal.WithLabelValues("disk-critical")), 0)
	require.InDelta(t, 2, testutil.ToFloat64(m.AlertsCreatedTotal.WithLabelValues(UnknownRule)), 0,
		"an unshipped rule is still counted — under one label, not one each")

	require.Len(t, seriesOf(t, reg, "opengate_alerts_created_total"), len(shippedRules)+1)
}

func seriesOf(t *testing.T, reg *prometheus.Registry, family string) []string {
	t.Helper()
	var out []string
	for _, series := range investigationSeries(t, reg) {
		if strings.HasPrefix(series, family+"{") {
			out = append(out, series)
		}
	}
	return out
}

func TestUnseededMetricsCountEveryRuleTheyAreGiven(t *testing.T) {
	t.Parallel()

	m := NewMetrics(prometheus.NewRegistry())
	m.ObserveAlertCreated("disk-critical")

	require.InDelta(t, 1, testutil.ToFloat64(m.AlertsCreatedTotal.WithLabelValues("disk-critical")), 0)
}
