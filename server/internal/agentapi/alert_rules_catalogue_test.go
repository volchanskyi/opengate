package agentapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// fakeRuleConfig is an in-memory rule store keyed by customer.
type fakeRuleConfig struct {
	bindings map[uuid.UUID][]rules.Binding
	rollouts map[uuid.UUID]map[string]rules.Rollout
	err      error
}

func customerRows[T any](f *fakeRuleConfig, rows map[uuid.UUID]T, org uuid.UUID) (T, error) {
	if f.err != nil {
		var none T
		return none, f.err
	}
	return rows[org], nil
}

func (f *fakeRuleConfig) ListBindings(_ context.Context, org uuid.UUID) ([]rules.Binding, error) {
	return customerRows(f, f.bindings, org)
}

func (f *fakeRuleConfig) ListRollouts(_ context.Context, org uuid.UUID) (map[string]rules.Rollout, error) {
	return customerRows(f, f.rollouts, org)
}

// staticTags gives every machine the same tags.
type staticTags map[string]string

func (s staticTags) TagsFor(context.Context, uuid.UUID) (map[string]string, error) {
	return s, nil
}

func newCatalogueProvider(t *testing.T, store RuleConfigStore, tags DeviceTagReader) *CatalogueAlertRuleProvider {
	t.Helper()
	cat, err := rules.Embedded()
	require.NoError(t, err)
	return NewCatalogueAlertRuleProvider(cat, store, tags, nil, nil, testLogger())
}

// orgBinding builds a binding covering one whole customer.
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

func diskBinding(org uuid.UUID, threshold float64) rules.Binding {
	return orgBinding(org, "disk-critical", map[string]float64{"threshold": threshold})
}

func fileServerBinding(org uuid.UUID, threshold float64) rules.Binding {
	b := diskBinding(org, threshold)
	b.Selector = rules.Selector{"role": "file-server"}
	return b
}

// bindingsFor wraps one customer's bindings in the store shape.
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

func assertDiskThreshold(t *testing.T, p *CatalogueAlertRuleProvider, scope settings.Scope, want float64, msg string) {
	t.Helper()
	assert.InEpsilon(t, want, mustResolve(t, p, scope)["disk-critical"].Threshold, 0.0001, msg)
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

func TestCatalogueProviderServesTheShippedPackByDefault(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	p := newCatalogueProvider(t, &fakeRuleConfig{}, nil)

	got, err := p.RulesFor(context.Background(), ladderFor(org))
	require.NoError(t, err)

	cat, err := rules.Embedded()
	require.NoError(t, err)
	watchingReadings := 0
	for _, def := range cat.All() {
		if !def.WatchesEvents() {
			watchingReadings++
		}
	}
	assert.Len(t, got.Rules, watchingReadings,
		"every shipped rule about a reading should reach a customer who configured nothing")

	indexed := byRuleID(got.Rules)
	disk, ok := indexed["disk-critical"]
	require.True(t, ok)
	assert.InEpsilon(t, 90.0, disk.Threshold, 0.0001)
	assert.Equal(t, "disk.used_percent", disk.Metric)
}

func TestCatalogueProviderAppliesACustomersBinding(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	scope := ladderFor(org)
	store := &fakeRuleConfig{bindings: bindingsFor(org, diskBinding(org, 95))}

	disk := mustResolve(t, newCatalogueProvider(t, store, nil), scope)["disk-critical"]
	assert.InEpsilon(t, 95.0, disk.Threshold, 0.0001, "the customer's threshold reaches the machine")
	assert.InEpsilon(t, 85.0, disk.Clear, 0.0001, "what they did not set stays what shipped")
}

func TestCatalogueProviderHonoursASelector(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := &fakeRuleConfig{bindings: bindingsFor(org, fileServerBinding(org, 98))}

	fileServer := newCatalogueProvider(t, store, staticTags{"role": "file-server"})
	assertDiskThreshold(t, fileServer, ladderFor(org), 98, "the named role takes the customer's number")

	workstation := newCatalogueProvider(t, store, staticTags{"role": "workstation"})
	assertDiskThreshold(t, workstation, ladderFor(org), 90,
		"a machine the selector does not name keeps the shipped number")
}

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

func TestCatalogueProviderResolvesLegacyMetricNamesEndToEnd(t *testing.T) {
	t.Parallel()

	const ruleYAML = `
  - id: %s
    version: 1
    severity: warning
    summary: Written before the vitals rename.
    metric: %s
    comparator: gte
    threshold: %d
    clear: 85
    sustain_secs: 300
    predicate: Instant
    group_by: [device]
    group_window_secs: 300`
	doc := "rules:" + fmt.Sprintf(ruleYAML, "legacy-memory", "mem.used", 95) +
		fmt.Sprintf(ruleYAML, "legacy-disk", "disk.used", 90) + "\n"
	legacy, err := rules.LoadCatalogue([]byte(doc), nil)
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

func TestCatalogueProviderKeepsCustomersApartInsideOneTenant(t *testing.T) {
	t.Parallel()

	tenant := uuid.New()
	contoso, fabrikam := uuid.New(), uuid.New()

	store := &fakeRuleConfig{
		bindings: bindingsFor(contoso, diskBinding(contoso, 98)),
		rollouts: map[uuid.UUID]map[string]rules.Rollout{
			contoso: {"cpu-saturated": {OrganizationID: contoso, RuleID: "cpu-saturated"}},
		},
	}
	p := newCatalogueProvider(t, store, nil)

	inTenant := func(org uuid.UUID) settings.Scope {
		return settings.Scope{DeviceID: uuid.New(), OrganizationID: org, TenantID: tenant}
	}

	assertDiskThreshold(t, p, inTenant(contoso), 98, "Contoso keeps its own number")
	assertDiskThreshold(t, p, inTenant(fabrikam), 90, "Contoso's threshold must not reach Fabrikam")

	contosoRules := mustResolve(t, p, inTenant(contoso))
	fabrikamRules := mustResolve(t, p, inTenant(fabrikam))

	assert.NotContains(t, contosoRules, "cpu-saturated", "Contoso switched this one off")
	assert.Contains(t, fabrikamRules, "cpu-saturated", "Fabrikam did not")
}

func TestCatalogueProviderReportsAnUnreadableStore(t *testing.T) {
	t.Parallel()

	boom := errors.New("database is down")
	p := newCatalogueProvider(t, &fakeRuleConfig{err: boom}, nil)

	got, err := p.RulesFor(context.Background(), ladderFor(uuid.New()))
	require.ErrorIs(t, err, boom)
	assert.Empty(t, got.Rules, "no ruleset is better than one that ignores a kill switch")
}

func TestCatalogueProviderServesShippedRulesWithoutACustomer(t *testing.T) {
	t.Parallel()

	p := newCatalogueProvider(t, &fakeRuleConfig{}, nil)
	noCustomer := settings.Scope{DeviceID: uuid.New(), TenantID: uuid.New()}
	assertDiskThreshold(t, p, noCustomer, 90, "the shipped number applies with no customer")
}

func TestCatalogueProviderSurvivesAFailingTagSource(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	store := &fakeRuleConfig{bindings: bindingsFor(org, diskBinding(org, 93), fileServerBinding(org, 98))}

	p := newCatalogueProvider(t, store, failingTags{})
	assertDiskThreshold(t, p, ladderFor(org), 93, "the customer's untargeted binding still applies")
}

type failingTags struct{}

func (failingTags) TagsFor(context.Context, uuid.UUID) (map[string]string, error) {
	return nil, errors.New("tag store unavailable")
}
