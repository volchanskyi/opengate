package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func budget(org uuid.UUID, customerHourly, machineHourly int) Limits {
	return Limits{
		OrganizationID:     org,
		OrganizationHourly: customerHourly,
		DeviceHourly:       machineHourly,
		UpdatedBy:          "ivan",
	}
}

func TestCeilingsAreEditableUpToAHardMaximum(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	for name, outside := range map[string]Limits{
		"a customer budget above the maximum": budget(org, MaxOrganizationHourlyCeiling+1, DefaultDeviceHourlyCeiling),
		"a machine budget above the maximum":  budget(org, DefaultOrganizationHourlyCeiling, MaxDeviceHourlyCeiling+1),
		"a customer budget of nothing":        budget(org, 0, DefaultDeviceHourlyCeiling),
		"a machine budget of nothing":         budget(org, DefaultOrganizationHourlyCeiling, 0),
		"no customer":                         budget(uuid.Nil, DefaultOrganizationHourlyCeiling, DefaultDeviceHourlyCeiling),
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, ValidateLimits(outside), ErrInvalidLimits)
		})
	}

	require.NoError(t, ValidateLimits(budget(org, MaxOrganizationHourlyCeiling, MaxDeviceHourlyCeiling)))
	require.NoError(t, ValidateLimits(DefaultLimits(org)))

	require.NoError(t, ValidateLimits(budget(org, 1, 1)),
		"a budget of one alert an hour is the smallest budget, not an absent one")
}

func TestAnUnconfiguredCustomerIsOnTheShippedBudget(t *testing.T) {
	t.Parallel()

	e := newEstate(t)

	got, err := e.alerts.Limits(e.ctx, e.org)
	require.NoError(t, err)
	assert.Equal(t, DefaultLimits(e.org), got)
}

func TestABudgetIsPerCustomer(t *testing.T) {
	t.Parallel()

	e := newEstate(t)
	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")

	require.NoError(t, e.alerts.UpsertLimits(e.ctx, budget(e.org, 2000, 50)))

	mine, err := e.alerts.Limits(e.ctx, e.org)
	require.NoError(t, err)
	assert.Equal(t, 2000, mine.OrganizationHourly)
	assert.Equal(t, 50, mine.DeviceHourly)

	theirs, err := e.alerts.Limits(e.ctx, other)
	require.NoError(t, err)
	assert.Equal(t, DefaultLimits(other), theirs,
		"raising one customer's budget must not raise another's")
}

func TestStoringABudgetPastTheMaximumIsRefused(t *testing.T) {
	t.Parallel()

	e := newEstate(t)

	err := e.alerts.UpsertLimits(e.ctx,
		budget(e.org, MaxOrganizationHourlyCeiling+1, DefaultDeviceHourlyCeiling))
	require.ErrorIs(t, err, ErrInvalidLimits)

	stored, err := e.alerts.Limits(e.ctx, e.org)
	require.NoError(t, err)
	assert.Equal(t, DefaultLimits(e.org), stored, "a refused write must leave nothing behind")
}

func TestLimitsRequireTenantScope(t *testing.T) {
	t.Parallel()

	e := newEstate(t)
	bare := context.Background()

	_, err := e.alerts.Limits(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	assert.ErrorIs(t, e.alerts.UpsertLimits(bare, DefaultLimits(e.org)), dbtx.ErrTenantRequired)
}

func TestALoweredBudgetTakesEffectAndStillCountsWhatItRefuses(t *testing.T) {
	t.Parallel()

	e := newEstate(t)
	require.NoError(t, e.alerts.UpsertLimits(e.ctx, budget(e.org, 3, DefaultDeviceHourlyCeiling)))

	e.seedHourOfAlerts(t, 3, e.now.Add(-10*time.Minute))

	e.record(t, e.alert(), CeilingSuppressed)
	assert.Equal(t, 3, e.count(t, qCustomerAlerts, e.org),
		"the budget in force is the retuned one, not the one the code ships")

	storm := e.openRoom(t, StormRuleID)
	assert.Equal(t, 1, storm.Occurrences,
		"what a lowered ceiling refuses is counted at whatever value it is set to")

	require.NoError(t, e.alerts.UpsertLimits(e.ctx,
		budget(e.org, DefaultOrganizationHourlyCeiling, DefaultDeviceHourlyCeiling)))
	e.record(t, e.variant(shifted(time.Minute)), Stored)
	assert.Equal(t, 4, e.count(t, qCustomerAlerts, e.org))
}
