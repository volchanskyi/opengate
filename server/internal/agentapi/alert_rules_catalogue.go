package agentapi

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// RuleConfigStore is the customer-mutable half of a rule: what they retuned and
// how far a rule has been rolled out to them.
type RuleConfigStore interface {
	// ListBindings returns every parameter override one customer has set.
	ListBindings(ctx context.Context, organizationID uuid.UUID) ([]rules.Binding, error)
	// ListRollouts returns one customer's rollout state keyed by rule id.
	ListRollouts(ctx context.Context, organizationID uuid.UUID) (map[string]rules.Rollout, error)
}

// DeviceTagReader supplies the tags a binding's selector matches a machine by.
type DeviceTagReader interface {
	TagsFor(ctx context.Context, deviceID uuid.UUID) (map[string]string, error)
}

// AlertLimitReader reads a customer's alert budget, whose per-machine half travels with the rules.
type AlertLimitReader interface {
	Limits(ctx context.Context, organizationID uuid.UUID) (alerts.Limits, error)
}

// FleetCounter counts a customer's machines, which sizes a rollout stage's canary floor.
type FleetCounter interface {
	Counts(ctx context.Context, organizationID uuid.UUID) (device.Counts, error)
}

// CatalogueAlertRuleProvider serves each machine the shipped catalogue as its
// customer has retuned it.
type CatalogueAlertRuleProvider struct {
	catalogue *rules.Catalogue
	store     RuleConfigStore
	tags      DeviceTagReader
	fleet     FleetCounter
	limits    AlertLimitReader
	logger    *slog.Logger
	memory    settingsMemory
}

// NewCatalogueAlertRuleProvider builds a provider over a catalogue and the customer-mutable
// state; nil tag, limit or fleet sources are optional.
func NewCatalogueAlertRuleProvider(
	catalogue *rules.Catalogue,
	store RuleConfigStore,
	tags DeviceTagReader,
	fleet FleetCounter,
	limits AlertLimitReader,
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

// RulesFor returns the ruleset for the machine at scope, reading its tags and customer's count;
// an unreadable store is an error, so the agent keeps its current rules.
func (p *CatalogueAlertRuleProvider) RulesFor(ctx context.Context, scope settings.Scope) (RuleSet, error) {
	if scope.OrganizationID == uuid.Nil {
		return p.shipped(rules.Device{Scope: scope}), nil
	}
	held, err := p.settingsFor(ctx, scope.OrganizationID)
	if err != nil {
		return RuleSet{}, err
	}
	return p.assemble(rules.Device{
		Scope:     scope,
		Tags:      p.tagsFor(ctx, scope.DeviceID),
		FleetSize: p.fleetSizeFor(ctx, scope.OrganizationID, held.rollouts),
	}, held), nil
}

// RulesForMachine returns the ruleset for a machine whose tags and fleet size its sign-in read.
func (p *CatalogueAlertRuleProvider) RulesForMachine(ctx context.Context, machine rules.Device) (RuleSet, error) {
	if machine.Scope.OrganizationID == uuid.Nil {
		return p.shipped(machine), nil
	}
	held, err := p.settingsFor(ctx, machine.Scope.OrganizationID)
	if err != nil {
		return RuleSet{}, err
	}
	return p.assemble(machine, held), nil
}

// shipped is the catalogue as it ships, for a machine with no customer.
func (p *CatalogueAlertRuleProvider) shipped(machine rules.Device) RuleSet {
	definitions := p.catalogue.All()
	return RuleSet{
		Rules:      resolveAll(definitions, machine, nil, nil),
		EventRules: wantedEventRules(definitions, uuid.Nil, nil),
	}
}

// assemble resolves the catalogue against the customer's held settings for one machine.
func (p *CatalogueAlertRuleProvider) assemble(machine rules.Device, held heldSettings) RuleSet {
	definitions := p.catalogue.All()
	return RuleSet{
		Rules:               resolveAll(definitions, machine, held.bindings, held.rollouts),
		EventRules:          wantedEventRules(definitions, machine.Scope.OrganizationID, held.rollouts),
		DeviceHourlyCeiling: held.deviceHourly,
		Version:             held.version,
	}
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
		// Event rules already run in the machine's log reader, so the reading evaluator skips them.
		if def.WatchesEvents() {
			continue
		}
		rollout := rules.RolloutFor(rollouts, machine.Scope.OrganizationID, def.ID)
		if !rollout.Reaches(machine.Scope.DeviceID, machine.FleetSize) {
			continue
		}
		out = append(out, rules.Resolve(def, machine, bindings))
	}
	return out
}

// wantedEventRules names the event rules the customer keeps enabled; staged reach does not apply
// because every machine already carries them.
func wantedEventRules(
	definitions []rules.Definition,
	organizationID uuid.UUID,
	rollouts map[string]rules.Rollout,
) map[string]struct{} {
	wanted := make(map[string]struct{})
	for _, def := range definitions {
		if !def.WatchesEvents() {
			continue
		}
		if rules.RolloutFor(rollouts, organizationID, def.ID).Delivers() {
			wanted[def.ID] = struct{}{}
		}
	}
	return wanted
}

// fleetSizeFor counts the customer's machines only when a rollout is staged; an unreadable count
// returns zero, so the rule reaches its declared share without a canary floor.
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

// tagsFor reads a machine's tags; an unreadable source loses only the tag-targeted bindings.
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
