package agentapi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

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

// CatalogueAlertRuleProvider serves each machine the shipped catalogue as its
// customer has retuned it.
type CatalogueAlertRuleProvider struct {
	catalogue *rules.Catalogue
	store     RuleConfigStore
	tags      DeviceTagSource
	logger    *slog.Logger
}

// NewCatalogueAlertRuleProvider builds a provider over a catalogue and the
// customer-mutable state. A nil tag source means selectors match nothing, which
// leaves every binding filed against a rung working as it should.
func NewCatalogueAlertRuleProvider(
	catalogue *rules.Catalogue,
	store RuleConfigStore,
	tags DeviceTagSource,
	logger *slog.Logger,
) *CatalogueAlertRuleProvider {
	return &CatalogueAlertRuleProvider{catalogue: catalogue, store: store, tags: tags, logger: logger}
}

// RulesFor returns the ruleset for the machine at scope.
//
// A store that cannot be read is reported rather than replaced with the shipped
// defaults. Substituting them would push rules that ignore whatever the customer
// set — including a kill switch, at exactly the moment somebody reached for one.
// The agent keeps the ruleset it already holds, so the cost of reporting is a
// ruleset that is not refreshed, not a machine that stops being watched.
func (p *CatalogueAlertRuleProvider) RulesFor(ctx context.Context, scope settings.Scope) ([]protocol.ThresholdRule, error) {
	definitions := p.catalogue.All()

	// A machine with no customer on its ladder has nothing to resolve against.
	// It takes the pack as it shipped rather than anyone else's numbers.
	if scope.OrganizationID == uuid.Nil {
		return resolveAll(definitions, rules.Device{Scope: scope}, nil, nil), nil
	}

	bindings, err := p.store.ListBindings(ctx, scope.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("read rule bindings: %w", err)
	}
	rollouts, err := p.store.ListRollouts(ctx, scope.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("read rule rollout: %w", err)
	}

	device := rules.Device{Scope: scope, Tags: p.tagsFor(ctx, scope.DeviceID)}
	return resolveAll(definitions, device, bindings, rollouts), nil
}

// resolveAll turns the definitions a customer is getting into wire rules.
func resolveAll(
	definitions []rules.Definition,
	device rules.Device,
	bindings []rules.Binding,
	rollouts map[string]rules.Rollout,
) []protocol.ThresholdRule {
	out := make([]protocol.ThresholdRule, 0, len(definitions))
	for _, def := range definitions {
		if !rules.RolloutFor(rollouts, device.Scope.OrganizationID, def.ID).Delivers() {
			continue
		}
		out = append(out, rules.Resolve(def, device, bindings))
	}
	return out
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
