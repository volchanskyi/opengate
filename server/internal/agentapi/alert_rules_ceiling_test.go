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

// What a machine is told about its own alert allowance, and what a stop does to
// a machine that was offline when somebody reached for it.

// fixedLimits answers with one customer's budget, or one error.
type fixedLimits struct {
	limits alerts.Limits
	err    error
}

func (f fixedLimits) Limits(context.Context, uuid.UUID) (alerts.Limits, error) {
	return f.limits, f.err
}

// told assembles what one machine is told: the rules it runs and the allowance
// it runs them under. Every case here differs only in what the stores answer, so
// the assembly is stated once.
func told(t *testing.T, store RuleConfigStore, limits AlertLimitReader, scope settings.Scope) RuleSet {
	t.Helper()
	cat, err := rules.Embedded()
	require.NoError(t, err)

	got, err := NewCatalogueAlertRuleProvider(cat, store, nil, nil, limits, testLogger()).
		RulesFor(context.Background(), scope)
	require.NoError(t, err)
	return got
}

// budgetOf is the customer budget half of that, for the cases that are about the
// allowance rather than about which rules arrive.
func budgetOf(t *testing.T, limits AlertLimitReader) RuleSet {
	t.Helper()
	return told(t, &fakeRuleConfig{}, limits, ladderFor(uuid.New()))
}

// The per-machine allowance is enforced on the machine, so it travels down with
// the rules. A screen that only wrote a database row would have changed nothing.
func TestTheCustomersMachineAllowanceTravelsWithTheRules(t *testing.T) {
	t.Parallel()

	got := budgetOf(t, fixedLimits{limits: alerts.Limits{
		OrganizationHourly: 1000,
		DeviceHourly:       42,
	}})

	assert.Equal(t, uint32(42), got.DeviceHourlyCeiling)
	assert.NotEmpty(t, got.Rules, "the allowance rides the rules rather than replacing them")
}

// A budget that cannot be read leaves the machine on the allowance it already
// has, and so does a deployment with no budget source. Pushing a zero would be
// indistinguishable from a customer who set nothing, and a guess would either
// silence a machine or uncap it off the back of a failed query.
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

// A stored allowance outside what the code allows is held inside it on the way
// out, not only on the way in. A row written before a maximum was tightened, or
// written past the API altogether, would otherwise hand a machine an allowance
// nobody may set.
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

// A stopped rule is off the machine on its next connection, without a release
// and without anything having been pushed to it while it was gone. A machine
// that was offline when somebody reached for the stop is the case that matters:
// it is the one nothing could have been delivered to.
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
