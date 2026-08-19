package agentapi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// Assembling the ruleset one machine actually gets.
//
// Three things decide it. The catalogue says what each rule is — its predicate,
// its window, and the numbers it ships with — and is compiled in, so it is the
// same everywhere. The customer's bindings retune those numbers down the tenancy
// ladder. The rollout state says whether the customer gets the rule at all.
//
// Which customer a machine belongs to is the whole of the separation: the
// bindings read are the ones filed against that customer, so Contoso's threshold
// cannot reach a Fabrikam machine even when the two share a tenant and a
// database read would have returned both.

// RuleConfigStore is the customer-mutable half of a rule: what they retuned and
// how far a rule has been rolled out to them.
type RuleConfigStore interface {
	// ListBindings returns every parameter override one customer has set.
	ListBindings(ctx context.Context, organizationID uuid.UUID) ([]rules.Binding, error)
	// ListRollouts returns one customer's stored rollout state, keyed by rule
	// id. A rule absent from the result has not been configured.
	ListRollouts(ctx context.Context, organizationID uuid.UUID) (map[string]rules.Rollout, error)
}

// DeviceTagSource supplies the tags a binding's selector picks a machine out by.
// It is optional: a machine with no tags simply matches the bindings that name
// none, which is every binding filed against a rung rather than a tag.
type DeviceTagSource interface {
	TagsFor(ctx context.Context, deviceID uuid.UUID) (map[string]string, error)
}

// AlertLimitSource reads a customer's alert budget, the per-machine half of
// which travels down with the rules. It is optional: without one every machine
// keeps the allowance it already has.
type AlertLimitSource interface {
	Limits(ctx context.Context, organizationID uuid.UUID) (alerts.Limits, error)
}

// FleetCounter counts a customer's estate, which is what sizes a stage: a
// canary's floor of a handful of machines cannot be worked out from a percentage
// alone. It is the fleet rollup the device repository already answers, so
// nothing new is queried to size a rollout.
type FleetCounter interface {
	Counts(ctx context.Context, organizationID uuid.UUID) (device.Counts, error)
}

// CatalogueAlertRuleProvider serves each machine the shipped catalogue as its
// customer has retuned it.
type CatalogueAlertRuleProvider struct {
	catalogue *rules.Catalogue
	store     RuleConfigStore
	tags      DeviceTagSource
	fleet     FleetCounter
	limits    AlertLimitSource
	logger    *slog.Logger
}

// NewCatalogueAlertRuleProvider builds a provider over a catalogue and the
// customer-mutable state. A nil tag source means selectors match nothing, which
// leaves every binding filed against a rung working as it should. A nil fleet
// source costs a staged rule its canary floor and nothing else.
func NewCatalogueAlertRuleProvider(
	catalogue *rules.Catalogue,
	store RuleConfigStore,
	tags DeviceTagSource,
	fleet FleetCounter,
	limits AlertLimitSource,
	logger *slog.Logger,
) *CatalogueAlertRuleProvider {
	return &CatalogueAlertRuleProvider{
		catalogue: catalogue,
		store:     store,
		tags:      tags,
		fleet:     fleet,
		limits:    limits,
		logger:    logger,
	}
}

// RulesFor returns the ruleset for the machine at scope.
//
// A store that cannot be read is reported rather than replaced with the shipped
// defaults. Substituting them would push rules that ignore whatever the customer
// set — including a kill switch, at exactly the moment somebody reached for one.
// The agent keeps the ruleset it already holds, so the cost of reporting is a
// ruleset that is not refreshed, not a machine that stops being watched.
func (p *CatalogueAlertRuleProvider) RulesFor(ctx context.Context, scope settings.Scope) (RuleSet, error) {
	definitions := p.catalogue.All()

	// A machine with no customer on its ladder has nothing to resolve against.
	// It takes the pack as it shipped rather than anyone else's numbers.
	if scope.OrganizationID == uuid.Nil {
		return RuleSet{Rules: resolveAll(definitions, rules.Device{Scope: scope}, nil, nil)}, nil
	}

	bindings, err := p.store.ListBindings(ctx, scope.OrganizationID)
	if err != nil {
		return RuleSet{}, fmt.Errorf("read rule bindings: %w", err)
	}
	rollouts, err := p.store.ListRollouts(ctx, scope.OrganizationID)
	if err != nil {
		return RuleSet{}, fmt.Errorf("read rule rollout: %w", err)
	}

	machine := rules.Device{
		Scope:     scope,
		Tags:      p.tagsFor(ctx, scope.DeviceID),
		FleetSize: p.fleetSizeFor(ctx, scope.OrganizationID, rollouts),
	}
	return RuleSet{
		Rules:               resolveAll(definitions, machine, bindings, rollouts),
		DeviceHourlyCeiling: p.ceilingFor(ctx, scope.OrganizationID),
	}, nil
}

// ceilingFor reads the customer's per-machine alert allowance. A budget that
// cannot be read leaves the machine on the allowance it already has: pushing a
// zero would be indistinguishable from a customer who set nothing, and pushing a
// guess would either silence a machine or uncap it, both from a failed query.
func (p *CatalogueAlertRuleProvider) ceilingFor(ctx context.Context, organizationID uuid.UUID) uint32 {
	if p.limits == nil {
		return 0
	}
	limits, err := p.limits.Limits(ctx, organizationID)
	if err != nil {
		p.logger.Warn("read customer alert budget failed",
			"organization_id", organizationID, "error", err)
		return 0
	}

	// Held to the maximum the code allows on the way out, not only on the way
	// in. A row written before a maximum was tightened, or written past the API
	// altogether, would otherwise hand a machine an allowance nobody may set.
	return clampNonNegativeUint32(min(limits.DeviceHourly, alerts.MaxDeviceHourlyCeiling))
}

// resolveAll turns the definitions a customer is getting into wire rules.
func resolveAll(
	definitions []rules.Definition,
	machine rules.Device,
	bindings []rules.Binding,
	rollouts map[string]rules.Rollout,
) []protocol.ThresholdRule {
	out := make([]protocol.ThresholdRule, 0, len(definitions))
	for _, def := range definitions {
		rollout := rules.RolloutFor(rollouts, machine.Scope.OrganizationID, def.ID)
		if !rollout.Reaches(machine.Scope.DeviceID, machine.FleetSize) {
			continue
		}
		out = append(out, rules.Resolve(def, machine, bindings))
	}
	return out
}

// fleetSizeFor counts the customer's estate, which sizes any stage they are
// mid-rollout on. It is read for those customers only: every rule at full reach
// — which is every customer who has staged nothing — needs no count, and paying
// for one on every machine's reconnect to size a stage nobody is in is a query
// per connection for nothing.
//
// A count that cannot be read costs the stage its canary floor and nothing else:
// the rule reaches the share it declares, never the estate. Guessing upward
// would spread a rule that is still being tried the moment a query failed.
func (p *CatalogueAlertRuleProvider) fleetSizeFor(ctx context.Context, organizationID uuid.UUID, rollouts map[string]rules.Rollout) int {
	if p.fleet == nil || !rules.NeedsFleetSize(rollouts) {
		return 0
	}
	counts, err := p.fleet.Counts(ctx, organizationID)
	if err != nil {
		p.logger.Warn("count estate for rule rollout failed",
			"organization_id", organizationID, "error", err)
		return 0
	}
	return counts.Total
}

// tagsFor reads a machine's tags. Tags narrow a binding rather than carry one,
// so a source that cannot answer costs the machine its targeted bindings and
// nothing else — losing the whole ruleset over it would be the larger harm.
func (p *CatalogueAlertRuleProvider) tagsFor(ctx context.Context, deviceID uuid.UUID) map[string]string {
	if p.tags == nil {
		return nil
	}
	tags, err := p.tags.TagsFor(ctx, deviceID)
	if err != nil {
		p.logger.Warn("read device tags for rule selectors failed", "device_id", deviceID, "error", err)
		return nil
	}
	return tags
}
