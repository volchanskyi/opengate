package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// fakeRuleConfig stands in for the Postgres rule store, so these cases are about
// what the provider does with bindings and rollout rather than about SQL. The
// store's own behavior is proven against a real database in internal/rules.
type fakeRuleConfig struct {
	bindings map[uuid.UUID][]rules.Binding
	rollouts map[uuid.UUID]map[string]rules.Rollout
	err      error
}

func (f *fakeRuleConfig) ListBindings(_ context.Context, organizationID uuid.UUID) ([]rules.Binding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.bindings[organizationID], nil
}

func (f *fakeRuleConfig) ListRollouts(_ context.Context, organizationID uuid.UUID) (map[string]rules.Rollout, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rollouts[organizationID], nil
}

// staticTags gives every machine the same tags, which is enough to exercise a
// selector without a tag store existing yet.
type staticTags map[string]string

func (s staticTags) TagsFor(context.Context, uuid.UUID) (map[string]string, error) {
	return s, nil
}

func newCatalogueProvider(t *testing.T, store RuleConfigStore, tags DeviceTagSource) *CatalogueAlertRuleProvider {
	t.Helper()
	cat, err := rules.Embedded()
	require.NoError(t, err)
	return NewCatalogueAlertRuleProvider(cat, store, tags, nil, nil, testLogger())
}

// orgBinding builds a binding covering one whole customer — the shape every
// case here needs, differing only in the rule and the numbers.
func orgBinding(org uuid.UUID, ruleID string, params map[string]float64) rules.Binding {
	return rules.Binding{
		ID:             uuid.New(),
		OrganizationID: org,
		RuleID:         ruleID,
		Level:          settings.LevelOrganization,
		LevelKey:       org,
		Params:         params,
	}
}

// bindingsFor is the store shape a single customer's bindings go in.
func bindingsFor(org uuid.UUID, b ...rules.Binding) map[uuid.UUID][]rules.Binding {
	return map[uuid.UUID][]rules.Binding{org: b}
}

// mustResolve reads the ruleset one machine gets, indexed by rule id.
func mustResolve(t *testing.T, p *CatalogueAlertRuleProvider, scope settings.Scope) map[string]protocol.ThresholdRule {
	t.Helper()
	got, err := p.RulesFor(context.Background(), scope)
	require.NoError(t, err)
	return byRuleID(got.Rules)
}

func ladderFor(org uuid.UUID) settings.Scope {
	return settings.Scope{
		DeviceID:       uuid.New(),
		SiteID:         uuid.New(),
		OrganizationID: org,
		TenantID:       uuid.New(),
	}
}

func byRuleID(got []protocol.ThresholdRule) map[string]protocol.ThresholdRule {
	out := make(map[string]protocol.ThresholdRule, len(got))
	for _, r := range got {
		out[r.ID] = r
	}
	return out
}

// A customer who has configured nothing gets the curated pack as it shipped.
func TestCatalogueProviderServesTheShippedPackByDefault(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	p := newCatalogueProvider(t, &fakeRuleConfig{}, nil)

	got, err := p.RulesFor(context.Background(), ladderFor(org))
	require.NoError(t, err)

	cat, err := rules.Embedded()
	require.NoError(t, err)
	assert.Len(t, got.Rules, len(cat.All()), "every shipped rule should reach a customer who configured nothing")

	indexed := byRuleID(got.Rules)
	disk, ok := indexed["disk-critical"]
	require.True(t, ok)
	assert.InEpsilon(t, 90.0, disk.Threshold, 0.0001)
	assert.Equal(t, "disk.used_percent", disk.Metric)
}

// A customer's binding retunes the rule that reaches their machines, and only
// the parameter they set.
func TestCatalogueProviderAppliesACustomersBinding(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	scope := ladderFor(org)
	store := &fakeRuleConfig{bindings: bindingsFor(org,
		orgBinding(org, "disk-critical", map[string]float64{"threshold": 95}))}

	disk := mustResolve(t, newCatalogueProvider(t, store, nil), scope)["disk-critical"]
	assert.InEpsilon(t, 95.0, disk.Threshold, 0.0001, "the customer's threshold reaches the machine")
	assert.InEpsilon(t, 85.0, disk.Clear, 0.0001, "what they did not set stays what shipped")
}

// A selector narrows a binding to the machines it names, which is what lets one
// customer rule cover a file server and a workstation differently.
func TestCatalogueProviderHonoursASelector(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	targeted := orgBinding(org, "disk-critical", map[string]float64{"threshold": 98})
	targeted.Selector = rules.Selector{"role": "file-server"}
	store := &fakeRuleConfig{bindings: bindingsFor(org, targeted)}

	fileServer := newCatalogueProvider(t, store, staticTags{"role": "file-server"})
	assert.InEpsilon(t, 98.0, mustResolve(t, fileServer, ladderFor(org))["disk-critical"].Threshold, 0.0001)

	workstation := newCatalogueProvider(t, store, staticTags{"role": "workstation"})
	assert.InEpsilon(t, 90.0, mustResolve(t, workstation, ladderFor(org))["disk-critical"].Threshold, 0.0001,
		"a machine the selector does not name keeps the shipped number")
}

// A rule the customer switched off, or that has been killed, stops reaching
// their machines at all. Everything else still does.
func TestCatalogueProviderWithholdsAStoppedRule(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	tests := []struct {
		name    string
		rollout rules.Rollout
	}{
		{"switched off", rules.Rollout{OrganizationID: org, RuleID: "disk-critical", RolloutPercent: 100}},
		{"killed", func() rules.Rollout {
			r := rules.DefaultRollout(org, "disk-critical")
			r.Kill = true
			return r
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeRuleConfig{rollouts: map[uuid.UUID]map[string]rules.Rollout{
				org: {"disk-critical": tc.rollout},
			}}
			indexed := mustResolve(t, newCatalogueProvider(t, store, nil), ladderFor(org))
			assert.NotContains(t, indexed, "disk-critical", "a stopped rule must not reach the fleet")
			assert.Contains(t, indexed, "cpu-saturated", "and must not take the others with it")
		})
	}
}

// A rule written under a name from before the vitals rename still reaches the
// dimension the fleet collects — the end-to-end form, through the real provider.
func TestCatalogueProviderResolvesLegacyMetricNamesEndToEnd(t *testing.T) {
	t.Parallel()

	legacy, err := rules.LoadCatalogue([]byte(`
rules:
  - id: legacy-memory
    version: 1
    summary: Written before the vitals rename.
    metric: mem.used
    comparator: gte
    threshold: 95
    clear: 85
    sustain_secs: 300
    predicate: Instant
    group_by: [device]
    group_window_secs: 300
  - id: legacy-disk
    version: 1
    summary: Written before the vitals rename.
    metric: disk.used
    comparator: gte
    threshold: 90
    clear: 85
    sustain_secs: 300
    predicate: Instant
    group_by: [device]
    group_window_secs: 300
`), nil)
	require.NoError(t, err)

	p := NewCatalogueAlertRuleProvider(legacy, &fakeRuleConfig{}, nil, nil, nil, testLogger())
	got, err := p.RulesFor(context.Background(), ladderFor(uuid.New()))
	require.NoError(t, err)

	indexed := byRuleID(got.Rules)
	assert.Equal(t, "mem.used_percent", indexed["legacy-memory"].Metric)
	assert.Equal(t, "disk.used_percent", indexed["legacy-disk"].Metric)
	for _, r := range got.Rules {
		_, ok := protocol.CanonicalRuleMetric(r.Metric)
		assert.Truef(t, ok, "%s reached the wire under %s", r.ID, r.Metric)
	}
}

// One customer's numbers must not reach another's machines, including when both
// customers sit inside a single tenant — the case a tenant-scoped database read
// does not catch on its own.
func TestCatalogueProviderKeepsCustomersApartInsideOneTenant(t *testing.T) {
	t.Parallel()

	tenant := uuid.New()
	contoso, fabrikam := uuid.New(), uuid.New()

	store := &fakeRuleConfig{
		bindings: bindingsFor(contoso,
			orgBinding(contoso, "disk-critical", map[string]float64{"threshold": 98})),
		rollouts: map[uuid.UUID]map[string]rules.Rollout{
			contoso: {"cpu-saturated": {OrganizationID: contoso, RuleID: "cpu-saturated"}},
		},
	}
	p := newCatalogueProvider(t, store, nil)

	inTenant := func(org uuid.UUID) settings.Scope {
		return settings.Scope{DeviceID: uuid.New(), OrganizationID: org, TenantID: tenant}
	}

	contosoRules := mustResolve(t, p, inTenant(contoso))
	fabrikamRules := mustResolve(t, p, inTenant(fabrikam))

	assert.InEpsilon(t, 98.0, contosoRules["disk-critical"].Threshold, 0.0001)
	assert.InEpsilon(t, 90.0, fabrikamRules["disk-critical"].Threshold, 0.0001,
		"Contoso's threshold must not reach Fabrikam")

	assert.NotContains(t, contosoRules, "cpu-saturated", "Contoso switched this one off")
	assert.Contains(t, fabrikamRules, "cpu-saturated", "Fabrikam did not")
}

// A store that cannot be read is reported rather than papered over. Pushing the
// shipped defaults instead would ignore a customer's kill switch at exactly the
// moment somebody reached for it.
func TestCatalogueProviderReportsAnUnreadableStore(t *testing.T) {
	t.Parallel()

	boom := errors.New("database is down")
	p := newCatalogueProvider(t, &fakeRuleConfig{err: boom}, nil)

	got, err := p.RulesFor(context.Background(), ladderFor(uuid.New()))
	require.ErrorIs(t, err, boom)
	assert.Empty(t, got.Rules, "no ruleset is better than one that ignores a kill switch")
}

// A machine with no customer on its ladder has nothing to resolve against, so it
// takes the shipped pack rather than another customer's numbers.
func TestCatalogueProviderServesShippedRulesWithoutACustomer(t *testing.T) {
	t.Parallel()

	p := newCatalogueProvider(t, &fakeRuleConfig{}, nil)
	noCustomer := settings.Scope{DeviceID: uuid.New(), TenantID: uuid.New()}
	assert.InEpsilon(t, 90.0, mustResolve(t, p, noCustomer)["disk-critical"].Threshold, 0.0001)
}

// Tags are a targeting aid, not a dependency: a tag source that fails leaves the
// machine matching only the bindings that name no tags, rather than losing its
// rules entirely.
func TestCatalogueProviderSurvivesAFailingTagSource(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	targeted := orgBinding(org, "disk-critical", map[string]float64{"threshold": 98})
	targeted.Selector = rules.Selector{"role": "file-server"}
	store := &fakeRuleConfig{bindings: bindingsFor(org,
		orgBinding(org, "disk-critical", map[string]float64{"threshold": 93}), targeted)}

	p := newCatalogueProvider(t, store, failingTags{})
	assert.InEpsilon(t, 93.0, mustResolve(t, p, ladderFor(org))["disk-critical"].Threshold, 0.0001,
		"the customer's untargeted binding still applies")
}

type failingTags struct{}

func (failingTags) TagsFor(context.Context, uuid.UUID) (map[string]string, error) {
	return nil, errors.New("tag store unavailable")
}
