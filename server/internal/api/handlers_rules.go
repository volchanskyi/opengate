package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

// The curated pack, and how much of an estate each rule is actually watching.
//
// This is a read and only a read. Rules are data in a bounded grammar compiled
// into the server, validated and cost-bounded in CI before they can reach a
// machine — an agent that ran server-supplied code would be a supply-chain
// weapon aimed at every customer estate at once. So there is no authoring
// surface here and the response is shaped to not imply one: what a customer may
// change is the numbers each rule declares tunable and whether it is rolled out,
// and that is what comes back beside what the rule watches.
//
// Coverage is the other half, and the reason this endpoint exists rather than
// the catalogue being a constant in the client. A rule quietly evaluating on
// half an estate while reading as healthy is the failure the accounting exists
// to make impossible, so every machine is exactly one thing for every rule and
// the four states always add up to the fleet they were counted against.

// errRulesUnavailable is a deployment wired without the compiled pack.
var errRulesUnavailable = errors.New("the rule catalogue is not configured on this server")

// ListRules implements StrictServerInterface.
func (s *Server) ListRules(ctx context.Context, request ListRulesRequestObject) (ListRulesResponseObject, error) {
	if s.ruleCatalogue == nil {
		return nil, errRulesUnavailable
	}
	organizationID := deref(request.Params.OrganizationId)

	// The fleet and the split of it are read together, because a coverage share
	// taken against a fleet counted a moment apart describes an estate nobody
	// was ever in.
	counts, err := s.devices.Counts(ctx, device.OrganizationID(organizationID))
	if err != nil {
		return nil, err
	}
	coverage := s.coverageFor(ctx, organizationID, counts.Total)
	rollouts := s.rolloutsFor(ctx, organizationID)

	definitions := s.ruleCatalogue.All()
	catalogue := RuleCatalogue{FleetSize: counts.Total, Rules: make([]Rule, 0, len(definitions))}
	for _, definition := range definitions {
		catalogue.Rules = append(catalogue.Rules, ruleToAPI(
			definition, rollouts[definition.ID], coverage[definition.ID], counts.Total))
	}
	return ListRules200JSONResponse(catalogue), nil
}

// coverageFor reads the split, answering an all-unknown fleet when nothing can
// report one. A deployment with no coverage source has heard from nobody, which
// is exactly what unknown means.
func (s *Server) coverageFor(
	ctx context.Context, organizationID uuid.UUID, fleetSize int,
) map[string]agentapi.RuleCoverageCounts {
	if s.ruleCoverage == nil {
		return nil
	}
	return s.ruleCoverage.RuleCoverage(ctx, organizationID, fleetSize)
}

// rolloutsFor reads how far each rule has reached. A read that failed leaves the
// rules reading as their defaults rather than failing the whole catalogue: the
// coverage half is what somebody opened this for, and it is still true.
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

// ruleToAPI renders one rule as an operator reads it.
//
// The predicate, its extra terms and its clear threshold are deliberately not
// here. They are the grammar the rule is written in, and putting them on a
// read-only surface invites the question of how to change them — which is the
// one thing this product does not do.
func ruleToAPI(
	definition rules.Definition, rollout rules.Rollout,
	coverage agentapi.RuleCoverageCounts, fleetSize int,
) Rule {
	out := Rule{
		Id:               definition.ID,
		Version:          definition.Version,
		Summary:          definition.Summary,
		Metric:           definition.Metric,
		Comparator:       RuleComparator(definition.ComparatorName),
		Threshold:        definition.Threshold,
		GroupBy:          orEmpty(definition.GroupBy),
		GroupWindowSecs:  int(definition.GroupWindowSecs),
		Evidence:         orEmpty(definition.Evidence),
		CoverageRequires: orEmpty(definition.CoverageRequires),
		Tunable:          tunableToAPI(definition),
		Rollout: RuleRollout{
			Enabled:        rollout.Enabled,
			RolloutPercent: rollout.RolloutPercent,
			Kill:           rollout.Kill,
		},
		Coverage: coverageToAPI(coverage, fleetSize),
	}
	if definition.SustainSecs > 0 {
		sustain := int(definition.SustainSecs)
		out.SustainSecs = &sustain
	}
	if rollout.CanaryGroup != "" {
		group := rollout.CanaryGroup
		out.Rollout.CanaryGroup = &group
	}
	return out
}

// tunableToAPI renders the numbers a customer may retune, each beside the value
// the catalogue ships — a bound with no starting point says how far it can move
// but not from where.
func tunableToAPI(definition rules.Definition) map[string]RuleParameterBounds {
	out := make(map[string]RuleParameterBounds, len(definition.Tunable))
	for name, bounds := range definition.Tunable {
		shipped, _ := definition.ShippedParam(name)
		out[name] = RuleParameterBounds{Min: bounds.Min, Max: bounds.Max, Shipped: shipped}
	}
	return out
}

// coverageToAPI renders the split, filling the remainder into unknown so the
// four states account for every machine in the estate. A rule nothing has
// reported on is watching none of the fleet, and reading that as an empty split
// would make it look like a rule with no estate to watch.
func coverageToAPI(coverage agentapi.RuleCoverageCounts, fleetSize int) RuleCoverage {
	unknown := fleetSize - coverage.Active - coverage.Throttled - coverage.Unsupported
	return RuleCoverage{
		Active:      coverage.Active,
		Throttled:   coverage.Throttled,
		Unsupported: coverage.Unsupported,
		Unknown:     max(unknown, 0),
	}
}

// orEmpty renders an absent list as an empty one, so a reader never has to tell
// null from [].
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
