package agentapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// AlertRuleProvider returns the threshold-alert ruleset for one machine, keyed by its scope.
type AlertRuleProvider interface {
	// RulesFor returns the rules for the machine at scope; an error means the caller pushes nothing.
	RulesFor(ctx context.Context, scope settings.Scope) (RuleSet, error)
}

// RuleSet is the rules one machine evaluates plus its hourly alert ceiling, where zero keeps
// the machine's current allowance.
type RuleSet struct {
	Rules               []protocol.ThresholdRule
	DeviceHourlyCeiling uint32
	// EventRules holds the log-event rule ids the customer keeps enabled; alerts from absent
	// ids are refused on arrival.
	EventRules map[string]struct{}
}

// StaticAlertRuleProvider serves a default ruleset with per-tenant overrides, keyed by tenant
// so one tenant's rules cannot reach another.
type StaticAlertRuleProvider struct {
	defaultRules []protocol.ThresholdRule
	byTenant     map[uuid.UUID][]protocol.ThresholdRule
}

// NewStaticAlertRuleProvider builds a provider that returns defaultRules for any
// tenant absent from byTenant. Both arguments are copied defensively.
func NewStaticAlertRuleProvider(defaultRules []protocol.ThresholdRule, byTenant map[uuid.UUID][]protocol.ThresholdRule) *StaticAlertRuleProvider {
	p := &StaticAlertRuleProvider{
		defaultRules: cloneRules(defaultRules),
		byTenant:     make(map[uuid.UUID][]protocol.ThresholdRule, len(byTenant)),
	}
	for tenant, rules := range byTenant {
		p.byTenant[tenant] = cloneRules(rules)
	}
	return p
}

// RulesFor returns a copy of the scope's tenant override, or the default set, and never fails.
func (p *StaticAlertRuleProvider) RulesFor(_ context.Context, scope settings.Scope) (RuleSet, error) {
	if rules, ok := p.byTenant[scope.TenantID]; ok {
		return RuleSet{Rules: cloneRules(rules)}, nil
	}
	return RuleSet{Rules: cloneRules(p.defaultRules)}, nil
}

// resolveAlertRuleProvider returns provider unchanged, or a default static
// provider (minimal ruleset for every tenant) when the caller supplied none.
func resolveAlertRuleProvider(provider AlertRuleProvider) AlertRuleProvider {
	if provider != nil {
		return provider
	}
	return NewStaticAlertRuleProvider(DefaultAlertRules(), nil)
}

// DefaultAlertRules returns the built-in sustained-saturation rules with hysteresis, each on a
// canonical vitals dimension.
func DefaultAlertRules() []protocol.ThresholdRule {
	return []protocol.ThresholdRule{
		{ID: "disk-critical", Metric: "disk.used_percent", Comparator: protocol.AlertComparatorGte, Threshold: 90, Clear: 85, SustainSecs: 300},
		{ID: "cpu-saturated", Metric: "cpu.total", Comparator: protocol.AlertComparatorGte, Threshold: 95, Clear: 85, SustainSecs: 300},
		{ID: "memory-pressure", Metric: "mem.used_percent", Comparator: protocol.AlertComparatorGte, Threshold: 95, Clear: 85, SustainSecs: 300},
	}
}

// cloneRules returns an independent copy so a caller can never mutate a
// provider's shared backing slice.
func cloneRules(rules []protocol.ThresholdRule) []protocol.ThresholdRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]protocol.ThresholdRule, len(rules))
	copy(out, rules)
	return out
}

// pushAlertRules delivers the ruleset resolved for this machine's scope; a nil provider is a no-op.
func (a *AgentConn) pushAlertRules(ctx context.Context) error {
	if a.alertRules == nil {
		return nil
	}
	scope := a.settingsScope(ctx)
	ruleset, err := a.alertRules.RulesFor(ctx, scope)
	if err != nil {
		return fmt.Errorf("assemble alert rules: %w", err)
	}
	// The remembered event rules and customer filter incoming alerts until the next push.
	a.rememberRuleset(scope.OrganizationID, ruleset.EventRules)
	return a.SendPushAlertRules(ctx, ruleset)
}

// rememberRuleset records what this connection was last given, under the same
// guard as the rest of the snapshot so a concurrent read cannot tear it.
func (a *AgentConn) rememberRuleset(organizationID uuid.UUID, wanted map[string]struct{}) {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	a.organizationID = organizationID
	a.wantedEventRules = wanted
}

// PushAlertRules re-resolves this machine's ruleset and delivers it over the live connection.
func (a *AgentConn) PushAlertRules(ctx context.Context) error {
	return a.pushAlertRules(ctx)
}

// settingsScope reads the machine's place in the tenancy ladder; a failed read keeps the rungs
// the connection knows, so the tenant boundary holds.
func (a *AgentConn) settingsScope(ctx context.Context) settings.Scope {
	known := settings.Scope{DeviceID: a.DeviceID, SiteID: a.SiteID, TenantID: a.TenantID}
	if a.settings == nil {
		return known
	}
	scope, err := a.settings.ScopeFor(ctx, a.DeviceID)
	if err != nil {
		a.logger.Warn("read device tenancy scope failed", "device_id", a.DeviceID, "error", err)
		return known
	}
	return scope
}

// RefreshAlertRules re-resolves each connected machine's ruleset for one customer and returns
// how many were reached; a failed push costs only that machine.
func (s *AgentServer) RefreshAlertRules(ctx context.Context, organizationID uuid.UUID) int {
	return s.refreshRules(ctx, func(meta AgentMeta) bool {
		return meta.OrganizationID == organizationID
	})
}

// RefreshAlertRulesForTenant refreshes the ruleset of every connected machine in one tenant.
func (s *AgentServer) RefreshAlertRulesForTenant(ctx context.Context, tenantID uuid.UUID) int {
	return s.refreshRules(ctx, func(meta AgentMeta) bool {
		return meta.TenantID == tenantID
	})
}

// refreshRules pushes to every selected machine and skips one never given a ruleset, whose scope
// is unknown.
func (s *AgentServer) refreshRules(ctx context.Context, selects func(AgentMeta) bool) int {
	reached := 0
	for _, conn := range s.ListConnectedAgents() {
		meta := conn.Meta()
		if meta.OrganizationID == uuid.Nil || !selects(meta) {
			continue
		}
		if err := conn.PushAlertRules(ctx); err != nil {
			s.logger.Warn("push changed alert rules failed",
				"device_id", meta.DeviceID, "error", err)
			continue
		}
		reached++
	}
	return reached
}
