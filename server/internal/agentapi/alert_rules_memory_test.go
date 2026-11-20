package agentapi

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// countingRuleConfig is a rule store that records the tenant each read ran in.
type countingRuleConfig struct {
	fakeRuleConfig
	mu      sync.Mutex
	readsIn []uuid.UUID
}

func (c *countingRuleConfig) ListBindings(ctx context.Context, org uuid.UUID) ([]rules.Binding, error) {
	tenant, _ := dbtx.TenantFromContext(ctx)
	c.mu.Lock()
	c.readsIn = append(c.readsIn, tenant.TenantID)
	c.mu.Unlock()
	return c.fakeRuleConfig.ListBindings(ctx, org)
}

func (c *countingRuleConfig) reads() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]uuid.UUID(nil), c.readsIn...)
}

// directory lists fixed customers, each in its own tenant.
type directory []CustomerRef

func (d directory) AllCustomers(context.Context) ([]CustomerRef, error) { return d, nil }

func (d directory) CustomersIn(ctx context.Context) ([]uuid.UUID, error) {
	tenant, _ := dbtx.TenantFromContext(ctx)
	var out []uuid.UUID
	for _, c := range d {
		if c.TenantID == tenant.TenantID {
			out = append(out, c.OrganizationID)
		}
	}
	return out, nil
}

// twoCustomers is Contoso and Fabrikam, each in a tenant of its own, Contoso warning on full
// disks at 70%.
type twoCustomers struct {
	store              *countingRuleConfig
	provider           *CatalogueAlertRuleProvider
	contoso, fabrikam  CustomerRef
	contosoCtx, fabCtx context.Context
}

func newTwoCustomers(t *testing.T) twoCustomers {
	t.Helper()
	contoso := CustomerRef{TenantID: uuid.New(), OrganizationID: uuid.New()}
	fabrikam := CustomerRef{TenantID: uuid.New(), OrganizationID: uuid.New()}
	store := &countingRuleConfig{fakeRuleConfig: fakeRuleConfig{
		bindings: bindingsFor(contoso.OrganizationID, diskBinding(contoso.OrganizationID, 70)),
	}}
	return twoCustomers{
		store: store, provider: newCatalogueProvider(t, store, nil),
		contoso: contoso, fabrikam: fabrikam,
		contosoCtx: dbtx.WithTenant(context.Background(), contoso.TenantID, false),
		fabCtx:     dbtx.WithTenant(context.Background(), fabrikam.TenantID, false),
	}
}

func (c twoCustomers) machineOf(ref CustomerRef) rules.Device {
	return rules.Device{Scope: settings.Scope{
		DeviceID: uuid.New(), OrganizationID: ref.OrganizationID, TenantID: ref.TenantID,
	}}
}

func diskThresholdIn(t *testing.T, set RuleSet) float64 {
	t.Helper()
	return byRuleID(set.Rules)["disk-critical"].Threshold
}

func TestSettingsLoadedAtStartAreReadInsideEachCustomersTenant(t *testing.T) {
	c := newTwoCustomers(t)

	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso, c.fabrikam}))

	assert.ElementsMatch(t, []uuid.UUID{c.contoso.TenantID, c.fabrikam.TenantID}, c.store.reads())
	set, err := c.provider.RulesForMachine(c.contosoCtx, c.machineOf(c.contoso))
	require.NoError(t, err)
	assert.InDelta(t, 70, diskThresholdIn(t, set), 0.001)
	assert.Len(t, c.store.reads(), 2, "a sign-in after the load reads nothing")
}

func TestAChangeReloadsItsCustomerAndMovesOnlyThatVersion(t *testing.T) {
	c := newTwoCustomers(t)
	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso, c.fabrikam}))
	before, beforeOther := c.provider.Version(c.contoso.OrganizationID), c.provider.Version(c.fabrikam.OrganizationID)

	c.store.bindings = bindingsFor(c.contoso.OrganizationID, diskBinding(c.contoso.OrganizationID, 60))
	require.NoError(t, c.provider.Reload(c.contosoCtx, c.contoso.OrganizationID))

	set, err := c.provider.RulesForMachine(c.contosoCtx, c.machineOf(c.contoso))
	require.NoError(t, err)
	assert.InDelta(t, 60, diskThresholdIn(t, set), 0.001)
	assert.NotEqual(t, before, c.provider.Version(c.contoso.OrganizationID))
	assert.Equal(t, set.Version, c.provider.Version(c.contoso.OrganizationID), "the ruleset names what it was built from")
	assert.Equal(t, beforeOther, c.provider.Version(c.fabrikam.OrganizationID), "another customer's settings stay as read")
}

func TestATenantWideChangeReloadsEveryCustomerOfTheTenant(t *testing.T) {
	c := newTwoCustomers(t)
	directoryOf := directory{c.contoso, c.fabrikam}
	require.NoError(t, c.provider.Load(context.Background(), directoryOf))

	c.store.bindings = bindingsFor(c.contoso.OrganizationID, diskBinding(c.contoso.OrganizationID, 55))
	require.NoError(t, c.provider.ReloadTenant(c.contosoCtx, directoryOf))

	set, err := c.provider.RulesForMachine(c.contosoCtx, c.machineOf(c.contoso))
	require.NoError(t, err)
	assert.InDelta(t, 55, diskThresholdIn(t, set), 0.001)
	assert.Equal(t, c.contoso.TenantID, c.store.reads()[2], "the reload reads inside the changed tenant")
	assert.Len(t, c.store.reads(), 3, "Fabrikam's tenant is not read again")
}

func TestACustomerReadOnFirstUseIsRemembered(t *testing.T) {
	c := newTwoCustomers(t)

	for range 3 {
		set, err := c.provider.RulesForMachine(c.contosoCtx, c.machineOf(c.contoso))
		require.NoError(t, err)
		assert.InDelta(t, 70, diskThresholdIn(t, set), 0.001)
	}

	assert.Equal(t, []uuid.UUID{c.contoso.TenantID}, c.store.reads())
}

func TestAFleetSizeIsAskedForOnlyWhereARolloutIsStaged(t *testing.T) {
	c := newTwoCustomers(t)
	c.store.rollouts = map[uuid.UUID]map[string]rules.Rollout{
		c.contoso.OrganizationID: {"disk-critical": {RuleID: "disk-critical", Enabled: true, RolloutPercent: 25}},
	}
	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso, c.fabrikam}))
	reads := len(c.store.reads())

	assert.True(t, c.provider.NeedsFleetSize(c.contoso.OrganizationID))
	assert.False(t, c.provider.NeedsFleetSize(c.fabrikam.OrganizationID))
	assert.False(t, c.provider.NeedsFleetSize(uuid.New()), "a customer the memory lacks has no rollout to size")
	assert.Len(t, c.store.reads(), reads, "the question reads nothing")
}

func TestAReloadTheStoreRefusesKeepsWhatWasHeld(t *testing.T) {
	c := newTwoCustomers(t)
	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso}))
	held := c.provider.Version(c.contoso.OrganizationID)

	c.store.err = errors.New("database unavailable")
	assert.Error(t, c.provider.Reload(c.contosoCtx, c.contoso.OrganizationID))

	c.store.err = nil
	set, err := c.provider.RulesForMachine(c.contosoCtx, c.machineOf(c.contoso))
	require.NoError(t, err)
	assert.InDelta(t, 70, diskThresholdIn(t, set), 0.001)
	assert.Equal(t, held, c.provider.Version(c.contoso.OrganizationID))
}

func TestALoadTheStoreRefusesFailsTheStart(t *testing.T) {
	c := newTwoCustomers(t)
	c.store.err = errors.New("database unavailable")

	assert.Error(t, c.provider.Load(context.Background(), directory{c.contoso}))
}
