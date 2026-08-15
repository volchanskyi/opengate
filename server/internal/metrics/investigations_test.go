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

// What the platform-monitoring series have to guarantee, proven against the
// registry they are actually exported from rather than argued from the
// declarations above them.
//
// One property carries the rest: these series are O(rules). A rule pack is five
// entries and stays that size for a release; a fleet is however many machines
// every customer between them runs. The moment a per-entity label appears here,
// the platform's own monitoring becomes the largest cardinality source in the
// system it exists to watch — and it would grow with exactly the thing nobody is
// watching it for.

// shippedRules stands in for the embedded catalogue: a handful of ids, fixed for
// a release, which is the whole reason these series can carry one.
var shippedRules = []string{
	"cpu-saturated", "disk-critical", "disk-slow", "io-stalled", "memory-pressure",
}

// discardLogger is the logger for cases whose subject is a metric rather than a
// line of output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// investigationSeries enumerates every label set the investigation metrics
// export, read back out of the registry a scrape would read. Enumerating what is
// exported is the point: a label declared and never used costs nothing, and a
// label used once costs a series forever.
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

// isInvestigationFamily reports whether a metric family is one of the five this
// file is about.
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

// labelSetOf renders one exported sample's labels, sorted, so two registries can
// be compared as text.
func labelSetOf(metric *dto.Metric) string {
	pairs := make([]string, 0, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		pairs = append(pairs, label.GetName()+"="+label.GetValue())
	}
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}

// investigationLabelNames is every label name the investigation series actually
// carry, which is what a per-entity label would show up in.
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

// fleetOf exports the investigation series as a fleet of size machines would:
// every rule evaluating on every machine, one room open per machine, and one
// alert raised per machine per rule.
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
	// One customer spent its hourly budget. The reason is a closed set, so a
	// storm on five thousand machines is the same one series it is on one.
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

// TestInvestigationSeriesAreInvariantToFleetSize is C11. One machine and five
// thousand machines export the same series — the values move, the cardinality
// does not. Asserted by enumerating what the registry hands a scrape, because
// the declarations cannot tell you which label values were ever used.
func TestInvestigationSeriesAreInvariantToFleetSize(t *testing.T) {
	t.Parallel()

	oneMachine := investigationSeries(t, fleetOf(t, 1, shippedRules))
	wholeEstate := investigationSeries(t, fleetOf(t, 5000, shippedRules))

	require.NotEmpty(t, oneMachine, "the investigation series must be exported at all")
	require.Equal(t, oneMachine, wholeEstate,
		"platform meta-monitoring is O(rules); a fleet five thousand times larger exports the same series")
}

// TestInvestigationSeriesGrowOnlyWithTheRuleCount is the other half of C11: the
// cardinality has exactly one input, and shipping a sixth rule is what moves it.
func TestInvestigationSeriesGrowOnlyWithTheRuleCount(t *testing.T) {
	t.Parallel()

	fivePack := investigationSeries(t, fleetOf(t, 1, shippedRules))
	sixPack := investigationSeries(t, fleetOf(t, 1, append(append([]string{}, shippedRules...), "net-saturated")))

	// One created-alert counter plus one gauge per coverage state.
	want := len(fivePack) + 1 + len(RuleCoverageStates())
	require.Len(t, sixPack, want,
		"a new rule adds its own series and nothing else")
}

// TestInvestigationSeriesCarryNoPerEntityLabel is the test that would notice a
// single misplaced label. A tenant, a customer or a machine on any of these
// turns O(rules) into O(rules × estate), and nothing else here would fail.
func TestInvestigationSeriesCarryNoPerEntityLabel(t *testing.T) {
	t.Parallel()

	names := investigationLabelNames(t, fleetOf(t, 5000, shippedRules))

	// Stated as an allow-list rather than a ban-list: a label nobody thought to
	// ban is exactly the one that would be added.
	require.Equal(t, []string{"reason", "rule_id", "state", "status"}, names,
		"the investigation series carry only closed vocabularies, never an entity id")
}

// TestInvestigationSeriesCoverTheirClosedVocabularies exports every value of
// every closed vocabulary from the start, zero-valued ones included. A missing
// series reads as "no data" on a dashboard, which is not the same answer as
// "none open" — and the two look identical exactly when somebody is checking
// whether a rollout raised anything.
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
		len(OpenIncidentStatuses())+1+len(shippedRules)*(1+len(RuleCoverageStates())),
		"seeding exports the whole vocabulary and nothing beyond it")
}

// TestOpenGaugesFallBackToZeroWhenTheQueueEmpties keeps a gauge from reading as
// the last non-empty answer forever. A status the aggregate stops reporting has
// nothing open in it, and a stale count there is a triage queue that looks
// permanently occupied.
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

// TestRuleCoverageGaugeClearsARuleThatStopsReporting keeps a withdrawn rule from
// claiming forever that it is watching an estate.
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

// TestCoverageOfAnUnshippedRuleIsNotExported keeps the fleet split bounded by
// the catalogue. Coverage is assembled from what agents report, so a rule id
// this build never shipped is endpoint input arriving at a gauge.
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

// TestAlertsCreatedIsBoundedByTheCatalogue is the same bound the WS-19 breach
// path applies to its metric label, on the label that would otherwise be minted
// by whatever an endpoint chose to call its rule.
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

	// Exactly the seeded vocabulary plus the single catch-all.
	require.Len(t, seriesOf(t, reg, "opengate_alerts_created_total"), len(shippedRules)+1)
}

// seriesOf enumerates one family's label sets.
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

// TestUnseededMetricsCountEveryRuleTheyAreGiven states what a build that has
// declared no vocabulary does. Nothing bounds the label there, so the seeding is
// the bound — which is why main wires it from the embedded catalogue.
func TestUnseededMetricsCountEveryRuleTheyAreGiven(t *testing.T) {
	t.Parallel()

	m := NewMetrics(prometheus.NewRegistry())
	m.ObserveAlertCreated("disk-critical")

	require.InDelta(t, 1, testutil.ToFloat64(m.AlertsCreatedTotal.WithLabelValues("disk-critical")), 0)
}
