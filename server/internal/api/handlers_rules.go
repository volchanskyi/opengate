package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

// errRulesUnavailable is a deployment wired without the compiled pack.
var errRulesUnavailable = errors.New("the rule catalogue is not configured on this server")

// ListRules implements StrictServerInterface as a read-only catalogue; each rule's four
// coverage states add up to the fleet.
func (s *Server) ListRules(ctx context.Context, request ListRulesRequestObject) (ListRulesResponseObject, error) {
	if s.ruleCatalogue == nil {
		return nil, errRulesUnavailable
	}
	organizationID := deref(request.Params.OrganizationId)

	// The fleet count and the coverage split are read together so shares match one estate.
	counts, err := s.devices.Counts(ctx, device.OrganizationID(organizationID))
	if err != nil {
		return nil, err
	}
	coverage := s.coverageFor(ctx, organizationID, counts.Total)
	// A rollout belongs to one customer; with none picked, the tenant's own is shown.
	customer, err := s.customerOrDefault(ctx, request.Params.OrganizationId)
	if err != nil {
		return nil, err
	}
	rollouts := s.rolloutsFor(ctx, customer)
	noise := s.noiseFor(ctx, organizationID)

	definitions := s.ruleCatalogue.All()
	catalogue := RuleCatalogue{FleetSize: counts.Total, Rules: make([]Rule, 0, len(definitions))}
	for _, definition := range definitions {
		catalogue.Rules = append(catalogue.Rules, ruleToAPI(definition, rollouts[definition.ID],
			coverage[definition.ID], counts.Total, noise[definition.ID]))
	}
	return ListRules200JSONResponse(catalogue), nil
}

// coverageFor reads the coverage split and answers an all-unknown fleet when no source exists.
func (s *Server) coverageFor(
	ctx context.Context, organizationID uuid.UUID, fleetSize int,
) map[string]agentapi.RuleCoverageCounts {
	if s.ruleCoverage == nil {
		return nil
	}
	return s.ruleCoverage.RuleCoverage(ctx, organizationID, fleetSize)
}

// rolloutsFor reads how far each rule has reached; a failed read leaves the rules at defaults.
func (s *Server) rolloutsFor(ctx context.Context, organizationID uuid.UUID) map[string]rules.Rollout {
	if s.ruleRollouts == nil {
		return nil
	}
	stored, err := s.ruleRollouts.ListRollouts(ctx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read rule rollout state failed",
			"organization_id", organizationID, "error", err)
		return nil
	}
	return stored
}

// ruleToAPI renders one rule as an operator reads it, omitting the predicate grammar.
func ruleToAPI(
	definition rules.Definition, rollout rules.Rollout,
	coverage agentapi.RuleCoverageCounts, fleetSize int, noise alerts.Noise,
) Rule {
	out := Rule{
		Id:               definition.ID,
		Version:          definition.Version,
		Kind:             ruleKindToAPI(definition),
		Severity:         IncidentSeverity(definition.Severity),
		Summary:          definition.Summary,
		GroupBy:          orEmpty(definition.GroupBy),
		GroupWindowSecs:  int(definition.GroupWindowSecs),
		Evidence:         orEmpty(definition.Evidence),
		CoverageRequires: orEmpty(definition.CoverageRequires),
		Tunable:          tunableToAPI(definition),
		Rollout:          rolloutToAPI(rollout),
		Coverage:         coverageToAPI(coverage, fleetSize),
		Noise:            noiseToAPI(noise),
	}
	// An event rule compares no number, so it carries no metric, comparator or threshold.
	if !definition.WatchesEvents() {
		metric, comparator, threshold := definition.Metric,
			RuleComparator(definition.ComparatorName), definition.Threshold
		out.Metric, out.Comparator, out.Threshold = &metric, &comparator, &threshold
	}
	if definition.SustainSecs > 0 {
		sustain := int(definition.SustainSecs)
		out.SustainSecs = &sustain
	}
	return out
}

// ruleKindToAPI says what a rule watches, in the vocabulary the screen reads.
func ruleKindToAPI(definition rules.Definition) RuleKind {
	if definition.WatchesEvents() {
		return Event
	}
	return Reading
}

// noiseFor reads each rule's noise for this customer; a failed read leaves every badge neutral.
func (s *Server) noiseFor(ctx context.Context, organizationID uuid.UUID) map[string]alerts.Noise {
	if s.alertBudget == nil {
		return nil
	}
	noise, err := s.alertBudget.RuleNoise(ctx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read rule noise failed",
			"organization_id", organizationID, "error", err)
		return nil
	}
	return noise
}

// tunableToAPI renders each retunable number's bounds beside its shipped value.
func tunableToAPI(definition rules.Definition) map[string]RuleParameterBounds {
	out := make(map[string]RuleParameterBounds, len(definition.Tunable))
	for name, bounds := range definition.Tunable {
		shipped, _ := definition.ShippedParam(name)
		out[name] = RuleParameterBounds{Min: bounds.Min, Max: bounds.Max, Shipped: shipped}
	}
	return out
}

// coverageToAPI renders the split, filling the remainder into unknown so the four states
// account for every machine in the estate.
func coverageToAPI(coverage agentapi.RuleCoverageCounts, fleetSize int) RuleCoverage {
	unknown := fleetSize - coverage.Active - coverage.Throttled - coverage.Unsupported
	return RuleCoverage{
		Active:      coverage.Active,
		Throttled:   coverage.Throttled,
		Unsupported: coverage.Unsupported,
		Unknown:     max(unknown, 0),
	}
}

// orEmpty renders an absent list as an empty one so clients never see null.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
