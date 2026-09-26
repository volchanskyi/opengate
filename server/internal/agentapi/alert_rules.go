package agentapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// AlertRuleProvider returns the threshold-alert ruleset for one machine. The
// argument is the machine's whole place in the tenancy ladder — itself, the
// site it is filed into, the customer that site belongs to, and the tenant —
// so a provider can answer for a customer or a site without the caller having
// to look either up. The tenant in that ladder is the connecting agent's
// authoritative one, which is what keeps one tenant's rules from reaching
// another.
type AlertRuleProvider interface {
	// RulesFor returns the rules to push to the machine at scope. An error
	// means the ruleset could not be assembled — the caller pushes nothing
	// rather than something that might ignore what the customer configured.
	RulesFor(ctx context.Context, scope settings.Scope) (RuleSet, error)
}

// RuleSet is everything one machine is told about detection: the rules it
// evaluates, and how many alerts it may raise in a rolling hour.
//
// The allowance is here rather than in a message of its own because it is
// enforced on the machine, and a machine that received new rules but not the
// budget they run under would be tuned by half. A ceiling of zero leaves the
// machine on the allowance it already has, which is what a deployment with no
// stored budget means.
type RuleSet struct {
	Rules               []protocol.ThresholdRule
	DeviceHourlyCeiling uint32
	// EventRules names which rules about the machine's own words this customer
	// still wants, keyed by rule id.
	//
	// They are not in Rules and never travel, because the machine's log reader
	// already carries them. What it cannot carry is the customer's decision to
	// stop one: the matching goes on whatever anybody set, so the stop is kept
	// here and applied where the alert arrives. Absent from this set means the
	// customer switched it off or it was killed, and the alert is refused under
	// its own counted reason rather than filed into a queue somebody chose to
	// stop watching.
	EventRules map[string]struct{}
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

// RulesFor returns a defensive copy of the ruleset for the scope's tenant, or
// the default set when that tenant has no override. It reads only the tenant
// rung; the narrower rungs are carried for the providers that resolve them. It
// reads nothing outside itself, so it never fails.
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

// DefaultAlertRules is the minimal built-in ruleset shipped to every tenant that
// has no custom configuration: sustained resource-saturation alerts with
// hysteresis, tuned conservatively because delivery is investigation-aid only.
// Each names the canonical vitals dimension it watches.
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

// pushAlertRules delivers the connecting agent's threshold-alert ruleset,
// resolved against the machine's own place in the tenancy ladder so a rule
// tuned for one customer or one office reaches the machines it was tuned for.
// The tenant in that ladder is the connection's authoritative one, so one
// tenant's rules never reach another. A nil provider is a no-op; a missing
// capability surfaces as a capability error the caller can ignore.
func (a *AgentConn) pushAlertRules(ctx context.Context) error {
	if a.alertRules == nil {
		return nil
	}
	scope := a.settingsScope(ctx)
	ruleset, err := a.alertRules.RulesFor(ctx, scope)
	if err != nil {
		return fmt.Errorf("assemble alert rules: %w", err)
	}
	// Kept until the next push. Rules about the machine's own words never
	// travel, so this is the only place the customer's decision to stop one can
	// be applied — and it is read here, beside the ruleset, rather than queried
	// again on every alert that arrives. The customer is remembered with it, so
	// a change that customer makes can find this connection again.
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

// PushAlertRules re-resolves this machine's ruleset and delivers it.
//
// It is what makes an administrator's change reach a machine that is already
// connected. A healthy link is held open indefinitely, so waiting for the next
// registration means waiting for something unrelated to break it — long enough
// for somebody to switch a rule off, watch the screen say so, and have it go on
// firing every night.
func (a *AgentConn) PushAlertRules(ctx context.Context) error {
	return a.pushAlertRules(ctx)
}

// settingsScope reads the machine's place in the tenancy ladder. Alerts and
// vitals arrive on this connection and need the right customer attached, so the
// walk happens here rather than being inferred later. A read that fails leaves
// the rungs this connection already knows for itself, which keeps the tenant
// boundary intact and simply loses the narrower targeting.
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

// RefreshAlertRules re-resolves and delivers the ruleset to every connected
// machine of one customer, and answers how many were reached.
//
// This is what an administrator's change rides out on. Each machine's ruleset
// is resolved for its own place in the tenancy ladder, so this is a resolve per
// machine rather than one ruleset copied about: a threshold aimed at the file
// servers must not reach the workstations beside them.
//
// A machine that cannot be written to costs itself and nobody else. A
// connection breaking mid-push is ordinary, and that machine is given the
// change as it reconnects — which is the same path an offline machine has
// always taken.
func (s *AgentServer) RefreshAlertRules(ctx context.Context, organizationID uuid.UUID) int {
	return s.refreshRules(ctx, func(meta AgentMeta) bool {
		return meta.OrganizationID == organizationID
	})
}

// RefreshAlertRulesForTenant does the same for every customer in one tenant. It
// is the delivery half of the tenant-wide stop, which exists because the reason
// to stop a rule is usually that it is wrong everywhere.
func (s *AgentServer) RefreshAlertRulesForTenant(ctx context.Context, tenantID uuid.UUID) int {
	return s.refreshRules(ctx, func(meta AgentMeta) bool {
		return meta.TenantID == tenantID
	})
}

// refreshRules pushes to every connected machine the filter selects. A machine
// that has never been given a ruleset has nothing to refresh and is skipped:
// its scope is not known yet, and it will be given the current one when it is.
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
