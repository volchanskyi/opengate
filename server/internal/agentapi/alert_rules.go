package agentapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// AlertRuleProvider returns the threshold-alert ruleset for one machine, keyed by its scope.
type AlertRuleProvider interface {
	// RulesFor returns the rules for the machine at scope; an error means the caller pushes nothing.
	RulesFor(ctx context.Context, scope settings.Scope) (RuleSet, error)
	// RulesForMachine returns the rules for a machine whose tags and fleet size are known.
	RulesForMachine(ctx context.Context, machine rules.Device) (RuleSet, error)
	// NeedsFleetSize reports whether a customer's rollouts size a stage by its machine count.
	NeedsFleetSize(organizationID uuid.UUID) bool
	// Version is the stamp of the customer settings a ruleset is built from; a change moves it.
	Version(organizationID uuid.UUID) uint64
}

// RuleSet is the rules one machine evaluates plus its hourly alert ceiling, where zero keeps
// the machine's current allowance.
type RuleSet struct {
	Rules               []protocol.ThresholdRule
	DeviceHourlyCeiling uint32
	// EventRules holds the log-event rule ids the customer keeps enabled; alerts from absent
	// ids are refused on arrival.
	EventRules map[string]struct{}
	// Version is the stamp of the customer settings the set was built from.
	Version uint64
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
	for tenant, set := range byTenant {
		p.byTenant[tenant] = cloneRules(set)
	}
	return p
}

// RulesFor returns a copy of the scope's tenant override, or the default set, and never fails.
func (p *StaticAlertRuleProvider) RulesFor(_ context.Context, scope settings.Scope) (RuleSet, error) {
	if set, ok := p.byTenant[scope.TenantID]; ok {
		return RuleSet{Rules: cloneRules(set)}, nil
	}
	return RuleSet{Rules: cloneRules(p.defaultRules)}, nil
}

// RulesForMachine is RulesFor at the machine's scope.
func (p *StaticAlertRuleProvider) RulesForMachine(ctx context.Context, machine rules.Device) (RuleSet, error) {
	return p.RulesFor(ctx, machine.Scope)
}

// NeedsFleetSize is false: a static set stages nothing.
func (p *StaticAlertRuleProvider) NeedsFleetSize(uuid.UUID) bool { return false }

// Version is zero: a static set never changes.
func (p *StaticAlertRuleProvider) Version(uuid.UUID) uint64 { return 0 }

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
func cloneRules(set []protocol.ThresholdRule) []protocol.ThresholdRule {
	if len(set) == 0 {
		return nil
	}
	out := make([]protocol.ThresholdRule, len(set))
	copy(out, set)
	return out
}

// pushAlertRules delivers the ruleset resolved for this machine's scope; a nil provider is a no-op.
func (a *AgentConn) pushAlertRules(ctx context.Context) error {
	if a.alertRules == nil {
		return nil
	}
	// The remembered event rules and customer filter incoming alerts until the next push.
	_, err := a.rulesNow(ctx)
	return err
}

// rememberRuleset records what this connection was last given, under the same
// guard as the rest of the snapshot so a concurrent read cannot tear it.
func (a *AgentConn) rememberRuleset(organizationID uuid.UUID, wanted map[string]struct{}) {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	a.organizationID = organizationID
	a.wantedEventRules = wanted
}

// maxSignInRulePushes bounds how often a sign-in pushes again behind settings that kept moving.
const maxSignInRulePushes = 3

// pushSignInRules delivers the ruleset for the place, tags and fleet size the sign-in read, and
// pushes again while the customer's settings changed after the set was built.
func (a *AgentConn) pushSignInRules(ctx context.Context, machine rules.Device) error {
	if a.alertRules == nil {
		return nil
	}
	ruleset, err := a.alertRules.RulesForMachine(ctx, machine)
	if err != nil {
		return fmt.Errorf("assemble alert rules: %w", err)
	}
	organizationID := machine.Scope.OrganizationID
	a.rememberRuleset(organizationID, ruleset.EventRules)
	if err := a.SendPushAlertRules(ctx, ruleset); err != nil {
		return err
	}
	for range maxSignInRulePushes - 1 {
		if a.alertRules.Version(organizationID) == ruleset.Version {
			return nil
		}
		if ruleset, err = a.rulesNow(ctx); err != nil {
			return err
		}
	}
	return nil
}

// rulesNow pushes the ruleset as the customer's settings stand now and returns it.
func (a *AgentConn) rulesNow(ctx context.Context) (RuleSet, error) {
	scope := a.settingsScope(ctx)
	ruleset, err := a.alertRules.RulesFor(ctx, scope)
	if err != nil {
		return RuleSet{}, fmt.Errorf("assemble alert rules: %w", err)
	}
	a.rememberRuleset(scope.OrganizationID, ruleset.EventRules)
	return ruleset, a.SendPushAlertRules(ctx, ruleset)
}

// PushAlertRules re-resolves this machine's ruleset and delivers it over the live connection.
func (a *AgentConn) PushAlertRules(ctx context.Context) error {
	return a.pushAlertRules(ctx)
}

// settingsScope reads the machine's place in the tenancy ladder; a failed read keeps the rungs
// the connection's sign-in established, so the machine stays on its own customer's rules.
func (a *AgentConn) settingsScope(ctx context.Context) settings.Scope {
	known := settings.Scope{DeviceID: a.DeviceID, SiteID: a.SiteID, TenantID: a.TenantID,
		OrganizationID: a.Meta().OrganizationID}
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

// settingsReloader is a provider holding customer settings in memory, read again on a change.
type settingsReloader interface {
	Reload(ctx context.Context, organizationID uuid.UUID) error
	ReloadTenant(ctx context.Context, directory CustomerDirectory) error
}

// RefreshAlertRules reads the customer's settings again and re-resolves each of its connected
// machines' rulesets, returning how many were reached; a failed push costs only that machine.
func (s *AgentServer) RefreshAlertRules(ctx context.Context, organizationID uuid.UUID) int {
	if reloader, ok := s.alertRules.(settingsReloader); ok {
		if err := reloader.Reload(ctx, organizationID); err != nil {
			s.logger.Error("read changed rule settings failed; machines keep the settings held",
				"organization_id", organizationID, "error", err)
		}
	}
	return s.refreshRules(ctx, func(meta AgentMeta) bool {
		return meta.OrganizationID == organizationID
	})
}

// RefreshAlertRulesForTenant reads every customer's settings in one tenant again and refreshes
// the ruleset of every connected machine in it.
func (s *AgentServer) RefreshAlertRulesForTenant(ctx context.Context, tenantID uuid.UUID) int {
	if reloader, ok := s.alertRules.(settingsReloader); ok && s.customers != nil {
		if err := reloader.ReloadTenant(dbtx.WithTenant(ctx, tenantID, false), s.customers); err != nil {
			s.logger.Error("read changed rule settings failed; machines keep the settings held",
				"tenant_id", tenantID, "error", err)
		}
	}
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
