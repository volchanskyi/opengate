package metrics

import (
	"context"
	"log/slog"
	"time"
)

// Platform meta-monitoring of the rule pack and the queue it feeds.
//
// These series answer one kind of question — is a rollout behaving, and is the
// triage queue draining — and they answer it about the whole install at once.
// Every label here is a closed vocabulary: the rule ids this build ships, the
// statuses an open incident can hold, the states a machine can be in for a rule.
// None of them is an entity.
//
// That is the whole design constraint. A tenant, a customer or a machine on any
// of these turns O(rules) into O(rules × estate), and the platform's own
// monitoring becomes the largest cardinality source in the system it exists to
// watch — growing with exactly the dimension nobody is watching it for. The
// per-device picture is the edge's, and it stays there.
//
// The two gauges are counts over tables that only ever grow, so they are
// refreshed on a timer from one aggregate each. Computing them inside the
// collector would put a full aggregate on every scrape of an endpoint that is
// scraped by more than one thing.

// The states a machine can be in for one rule. Together they always add up to
// the fleet, which is why all four are exported: three of them would make a rule
// look like it was watching a smaller estate than it is.
const (
	// CoverageActive counts machines evaluating the rule.
	CoverageActive = "active"
	// CoverageThrottled counts machines that stopped evaluating it because it
	// cost them more than its allowance — the rule was written wrong.
	CoverageThrottled = "throttled"
	// CoverageUnsupported counts machines that cannot evaluate it at all: a
	// standing hole in an estate's monitoring rather than a liveness reading.
	CoverageUnsupported = "unsupported"
	// CoverageUnknown counts machines that have reported nothing.
	CoverageUnknown = "unknown"
)

// Where an incident that is not over can stand. A resolved incident is not open
// work, so it is not a series on a gauge that says it is.
const (
	// IncidentNew is the triage queue: incidents nobody has picked up.
	IncidentNew = "new"
	// IncidentAcknowledged is the ones somebody has taken.
	IncidentAcknowledged = "acknowledged"
	// IncidentInvestigating is the ones being worked.
	IncidentInvestigating = "investigating"
)

// UnknownRule is the one label value every rule id outside the shipped
// catalogue is counted under. It is deliberately not pre-created: the ingest
// path refuses an alert naming a rule this build has no definition for, so this
// series appearing at all means something reached the counter that should not
// have — and one catch-all series is what keeps finding that out from costing a
// label value per rule id an endpoint invented.
const UnknownRule = "unknown"

// openIncidentStatuses and ruleCoverageStates are the closed vocabularies the
// gauges are exported over. Every value is written on every refresh, present in
// the answer or not: a missing series reads as "no data" on a dashboard, which
// is not the same answer as "none open", and the two look identical exactly when
// somebody is checking whether a rollout raised anything.
var (
	openIncidentStatuses = []string{IncidentNew, IncidentAcknowledged, IncidentInvestigating}
	ruleCoverageStates   = []string{CoverageActive, CoverageThrottled, CoverageUnsupported, CoverageUnknown}
)

// OpenIncidentStatuses is every status an open incident can hold.
func OpenIncidentStatuses() []string {
	return append([]string(nil), openIncidentStatuses...)
}

// RuleCoverageStates is every state a machine can be in for one rule.
func RuleCoverageStates() []string {
	return append([]string(nil), ruleCoverageStates...)
}

// ruleVocabulary is the rule ids this build ships, held as both the order they
// are exported in and the set membership is tested against.
type ruleVocabulary struct {
	ids []string
	set map[string]struct{}
}

// SeedRuleVocabulary declares the rule ids this build ships and exports a
// zero-valued series for each of them.
//
// It does two jobs, and both matter. A rule that has never fired reads as zero
// rather than as a missing series, so a rollout that raised nothing is
// distinguishable from a scrape that found nothing. And the declared set becomes
// the bound on the rule_id label: rule ids travel to the agent and come back on
// alerts and coverage reports, so without a bound the endpoint would decide this
// server's cardinality.
//
// Called once at start-up from the embedded catalogue, before any agent
// connects.
func (m *Metrics) SeedRuleVocabulary(ruleIDs []string) {
	vocabulary := &ruleVocabulary{
		ids: append([]string(nil), ruleIDs...),
		set: make(map[string]struct{}, len(ruleIDs)),
	}
	for _, ruleID := range ruleIDs {
		vocabulary.set[ruleID] = struct{}{}
		m.AlertsCreatedTotal.WithLabelValues(ruleID)
		for _, state := range ruleCoverageStates {
			m.RuleCoverage.WithLabelValues(ruleID, state)
		}
	}
	m.rules.Store(vocabulary)
}

// ObserveAlertCreated counts one alert that became a stored row, under the rule
// that raised it.
//
// Only a stored row: a reconnect replaying an alert already held changed
// nothing, and a refusal past the customer's ceiling stored nothing at all.
// Counting either would inflate the alerts-per-device-per-day figure the
// ceilings and the evidence projection are both sized against — which is the
// number this counter exists to measure rather than assume.
func (m *Metrics) ObserveAlertCreated(ruleID string) {
	m.AlertsCreatedTotal.WithLabelValues(m.boundedRuleID(ruleID)).Inc()
}

// boundedRuleID maps a rule id onto the declared vocabulary, folding anything
// outside it into the single catch-all. A build that has declared no vocabulary
// counts what it is given, which is why start-up seeds one.
func (m *Metrics) boundedRuleID(ruleID string) string {
	vocabulary := m.rules.Load()
	if vocabulary == nil {
		return ruleID
	}
	if _, shipped := vocabulary.set[ruleID]; shipped {
		return ruleID
	}
	return UnknownRule
}

// exportedRules is which rule ids a coverage refresh writes: the declared
// vocabulary when there is one, and otherwise whatever was reported.
func (m *Metrics) exportedRules(reported map[string]map[string]int) []string {
	if vocabulary := m.rules.Load(); vocabulary != nil {
		return vocabulary.ids
	}
	ids := make([]string, 0, len(reported))
	for ruleID := range reported {
		ids = append(ids, ruleID)
	}
	return ids
}

// InvestigationSource supplies the two aggregates the gauges are refreshed from.
// Each is one read of the whole install — no tenant scope, because these series
// carry no tenant: a triage queue in a tenant nobody is currently serving
// requests for is still a triage queue.
type InvestigationSource struct {
	// OpenInvestigations returns how many incidents are open in each status,
	// and how many alerts are sitting in them.
	OpenInvestigations func(ctx context.Context) (map[string]int, int, error)
	// FleetRuleCoverage returns, per rule id, how many machines are in each
	// coverage state.
	FleetRuleCoverage func(ctx context.Context) (map[string]map[string]int, error)
}

// StartInvestigationsUpdater refreshes the investigation gauges from src on a
// timer, starting with one pass before the first tick. It stops when the context
// is cancelled.
func StartInvestigationsUpdater(
	ctx context.Context, m *Metrics, src InvestigationSource, logger *slog.Logger, interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	refreshInvestigations(ctx, m, src, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshInvestigations(ctx, m, src, logger)
		}
	}
}

// refreshInvestigations reads both aggregates once and restates every gauge from
// what came back.
func refreshInvestigations(ctx context.Context, m *Metrics, src InvestigationSource, logger *slog.Logger) {
	refreshOpenWork(ctx, m, src, logger)
	refreshRuleCoverage(ctx, m, src, logger)
}

// refreshOpenWork restates how much unresolved work the investigation tables
// hold. A read that failed leaves the previous answer standing: a database that
// is briefly unreachable is not an empty triage queue, and the alert rule
// watching these gauges must not fire on the difference.
func refreshOpenWork(ctx context.Context, m *Metrics, src InvestigationSource, logger *slog.Logger) {
	if src.OpenInvestigations == nil {
		return
	}
	byStatus, openAlerts, err := src.OpenInvestigations(ctx)
	if err != nil {
		logger.Warn("metrics: failed to read open investigations", "error", err)
		return
	}
	// Written from the vocabulary rather than from the answer, so a status the
	// aggregate stopped reporting falls back to zero instead of standing at the
	// last count it held — a triage queue that looks permanently occupied is
	// worse than one that reads empty.
	for _, status := range openIncidentStatuses {
		m.IncidentsOpen.WithLabelValues(status).Set(float64(byStatus[status]))
	}
	m.AlertsOpen.Set(float64(openAlerts))
}

// refreshRuleCoverage restates how much of the fleet each rule is watching. Same
// two properties as above: a rule that stops being reported on reads as watching
// nothing, and a read that failed leaves the previous answer standing.
func refreshRuleCoverage(ctx context.Context, m *Metrics, src InvestigationSource, logger *slog.Logger) {
	if src.FleetRuleCoverage == nil {
		return
	}
	byRule, err := src.FleetRuleCoverage(ctx)
	if err != nil {
		logger.Warn("metrics: failed to read fleet rule coverage", "error", err)
		return
	}
	for _, ruleID := range m.exportedRules(byRule) {
		states := byRule[ruleID]
		for _, state := range ruleCoverageStates {
			m.RuleCoverage.WithLabelValues(ruleID, state).Set(float64(states[state]))
		}
	}
}
