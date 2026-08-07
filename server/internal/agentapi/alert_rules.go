package agentapi

import (
	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// AlertRuleProvider returns the WS-19 threshold-alert ruleset for one
// tenant. The server pushes only the connecting agent's authoritative
// tenant's rules (see AgentConn.pushAlertRules), so the lookup key is the trust
// boundary — one tenant's rules never reach another.
type AlertRuleProvider interface {
	// RulesFor returns the rules to push to an agent enrolled in tenantID.
	RulesFor(tenantID uuid.UUID) []protocol.ThresholdRule
}

// StaticAlertRuleProvider serves a minimal default ruleset to every tenant, with
// optional per-tenant overrides. It is the in-memory delivery mechanism for WS-19:
// rules are server configuration rather than a tenant Postgres table, and the
// per-tenant keying makes cross-tenant leakage structurally impossible.
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

// RulesFor returns a defensive copy of tenantID's ruleset, or the default set when
// the tenant has no override.
func (p *StaticAlertRuleProvider) RulesFor(tenantID uuid.UUID) []protocol.ThresholdRule {
	if rules, ok := p.byTenant[tenantID]; ok {
		return cloneRules(rules)
	}
	return cloneRules(p.defaultRules)
}

// resolveAlertRuleProvider returns provider unchanged, or a default static
// provider (minimal ruleset for every tenant) when the caller supplied none.
func resolveAlertRuleProvider(provider AlertRuleProvider) AlertRuleProvider {
	if provider != nil {
		return provider
	}
	return NewStaticAlertRuleProvider(DefaultAlertRules(), nil)
}

// DefaultAlertRules is the minimal built-in ruleset shipped to every tenant that
// has no custom configuration: sustained resource-saturation alerts with
// hysteresis, tuned conservatively because delivery is investigation-aid only.
func DefaultAlertRules() []protocol.ThresholdRule {
	return []protocol.ThresholdRule{
		{ID: "disk-critical", Metric: "disk.used", Comparator: protocol.AlertComparatorGte, Threshold: 90, Clear: 85, SustainSecs: 300},
		{ID: "cpu-saturated", Metric: "cpu.total", Comparator: protocol.AlertComparatorGte, Threshold: 95, Clear: 85, SustainSecs: 300},
		{ID: "memory-pressure", Metric: "mem.used", Comparator: protocol.AlertComparatorGte, Threshold: 95, Clear: 85, SustainSecs: 300},
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
