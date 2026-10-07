package metrics

import (
	"context"
	"log/slog"
	"time"
)

// The states a machine can be in for one rule; all four are exported so they sum to the fleet.
const (
	// CoverageActive counts machines evaluating the rule.
	CoverageActive = "active"
	// CoverageThrottled counts machines that stopped evaluating the rule over its cost allowance.
	CoverageThrottled = "throttled"
	// CoverageUnsupported counts machines that cannot evaluate the rule at all.
	CoverageUnsupported = "unsupported"
	// CoverageUnknown counts machines that have reported nothing.
	CoverageUnknown = "unknown"
)

// The statuses of an incident that is not resolved.
const (
	// IncidentNew is the triage queue: incidents nobody has picked up.
	IncidentNew = "new"
	// IncidentAcknowledged counts incidents somebody has taken.
	IncidentAcknowledged = "acknowledged"
	// IncidentInvestigating counts incidents being worked.
	IncidentInvestigating = "investigating"
)

// UnknownRule is the single label value for every rule id outside the shipped catalogue.
// It is created only on use, so its presence means an unknown rule reached the counter.
const UnknownRule = "unknown"

// The closed label vocabularies; every value is written on every refresh so empty reads 0.
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

// ruleVocabulary holds the shipped rule ids in export order and as a membership set.
type ruleVocabulary struct {
	ids []string
	set map[string]struct{}
}

// SeedRuleVocabulary declares the shipped rule ids, exports a zero-valued series for each,
// and bounds the rule_id label to that set. It runs once at start-up before any agent connects.
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

// ObserveAlertCreated counts one alert that became a stored row, under its rule.
// Replays and refusals past the ceiling are not counted.
func (m *Metrics) ObserveAlertCreated(ruleID string) {
	m.AlertsCreatedTotal.WithLabelValues(m.boundedRuleID(ruleID)).Inc()
}

// boundedRuleID maps a rule id onto the declared vocabulary, folding anything outside it
// into the catch-all; with no vocabulary declared it returns the id unchanged.
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

// exportedRules returns the rule ids a coverage refresh writes: the declared vocabulary,
// or the reported ids when none is declared.
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

// InvestigationSource supplies the two aggregates the gauges refresh from.
// Each reads the whole install with no tenant scope, since the series carry no tenant.
type InvestigationSource struct {
	// OpenInvestigations returns open incidents per status and the alerts sitting in them.
	OpenInvestigations func(ctx context.Context) (map[string]int, int, error)
	// FleetRuleCoverage returns, per rule id, the machines in each coverage state.
	FleetRuleCoverage func(ctx context.Context) (map[string]map[string]int, error)
}

// StartInvestigationsUpdater refreshes the investigation gauges from src on a timer,
// starting with one pass, until the context is cancelled.
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

// refreshOpenWork restates the unresolved-work gauges; a failed read keeps the previous
// answer so an unreachable database never reads as an empty triage queue.
func refreshOpenWork(ctx context.Context, m *Metrics, src InvestigationSource, logger *slog.Logger) {
	if src.OpenInvestigations == nil {
		return
	}
	byStatus, openAlerts, err := src.OpenInvestigations(ctx)
	if err != nil {
		logger.Warn("metrics: failed to read open investigations", "error", err)
		return
	}
	// Iterating the vocabulary resets a status the aggregate stopped reporting to zero.
	for _, status := range openIncidentStatuses {
		m.IncidentsOpen.WithLabelValues(status).Set(float64(byStatus[status]))
	}
	m.AlertsOpen.Set(float64(openAlerts))
}

// refreshRuleCoverage restates the per-rule coverage gauges; an unreported rule reads zero
// and a failed read keeps the previous answer.
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
