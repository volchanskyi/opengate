package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// fixedLimits answers with one customer's budget, or one error.
type fixedLimits struct {
	limits alerts.Limits
	err    error
}

func (f fixedLimits) Limits(context.Context, uuid.UUID) (alerts.Limits, error) {
	return f.limits, f.err
}

// told assembles the rules and the allowance one machine receives.
func told(t *testing.T, store RuleConfigStore, limits AlertLimitReader, scope settings.Scope) RuleSet {
	t.Helper()
	cat, err := rules.Embedded()
	require.NoError(t, err)

	got, err := NewCatalogueAlertRuleProvider(cat, store, nil, nil, limits, testLogger()).
		RulesFor(context.Background(), scope)
	require.NoError(t, err)
	return got
}

// budgetOf returns the ruleset a new customer's machine gets under the given alert limit reader.
func budgetOf(t *testing.T, limits AlertLimitReader) RuleSet {
	t.Helper()
	return told(t, &fakeRuleConfig{}, limits, ladderFor(uuid.New()))
}

func TestTheCustomersMachineAllowanceTravelsWithTheRules(t *testing.T) {
	t.Parallel()

	got := budgetOf(t, fixedLimits{limits: alerts.Limits{
		OrganizationHourly: 1000,
		DeviceHourly:       42,
	}})

	assert.Equal(t, uint32(42), got.DeviceHourlyCeiling)
	assert.NotEmpty(t, got.Rules, "the allowance rides the rules rather than replacing them")
}

func TestAnUnknownBudgetLeavesTheMachineOnItsCurrentAllowance(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]AlertLimitReader{
		"a budget that cannot be read": fixedLimits{err: errors.New("database is down")},
		"no budget source at all":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			got := budgetOf(t, source)
			assert.Zero(t, got.DeviceHourlyCeiling)
			assert.NotEmpty(t, got.Rules, "an unreadable budget must not cost the machine its rules")
		})
	}
}

func TestAnAllowanceOutsideTheAllowedRangeIsHeldInsideIt(t *testing.T) {
	t.Parallel()

	for name, stored := range map[string]int{
		"past the maximum":     alerts.MaxDeviceHourlyCeiling + 5_000,
		"a negative allowance": -1,
	} {
		t.Run(name, func(t *testing.T) {
			got := budgetOf(t, fixedLimits{limits: alerts.Limits{DeviceHourly: stored}})
			assert.LessOrEqual(t, got.DeviceHourlyCeiling, uint32(alerts.MaxDeviceHourlyCeiling))
		})
	}
}

func TestAStoppedRuleIsGoneWhenTheMachineComesBack(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	stopped := rules.DefaultRollout(org, "disk-critical")
	stopped.Kill = true

	got := told(t,
		&fakeRuleConfig{rollouts: map[uuid.UUID]map[string]rules.Rollout{
			org: {"disk-critical": stopped},
		}},
		nil,
		settings.Scope{DeviceID: uuid.New(), OrganizationID: org, TenantID: uuid.New()})

	assert.NotContains(t, byRuleID(got.Rules), "disk-critical",
		"a machine coming back online must not be handed a rule somebody stopped")
	assert.Contains(t, byRuleID(got.Rules), "cpu-saturated",
		"stopping one rule stops one rule")
}
