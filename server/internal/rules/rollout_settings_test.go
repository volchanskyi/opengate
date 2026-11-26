package rules

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestARetunedPopulationSizesTheNextStage(t *testing.T) {
	t.Parallel()

	r := DefaultRollout(uuid.New(), "disk-critical")
	r.CanaryPercent, r.StagedPercent = 5, 25

	assert.Equal(t, 5, r.PercentForStage(StageCanary))
	assert.Equal(t, 25, r.PercentForStage(StageStaged))
	assert.Equal(t, 100, r.PercentForStage(StageFull))

	r.RolloutPercent = 5
	assert.Equal(t, StageCanary, r.Stage())
	r.RolloutPercent = 25
	assert.Equal(t, StageStaged, r.Stage())
	r.RolloutPercent = 100
	assert.Equal(t, StageFull, r.Stage())

	assert.True(t, NeedsFleetSize(map[string]Rollout{"disk-critical": {
		Enabled: true, RolloutPercent: 25, CanaryPercent: 5, StagedPercent: 25,
	}}))
}

func TestRolloutSettingsAreBounded(t *testing.T) {
	t.Parallel()

	base := func() Rollout { return DefaultRollout(uuid.New(), "disk-critical") }

	for _, tc := range []struct {
		name  string
		apply func(*Rollout)
	}{
		{"a canary reaching fewer than nobody", func(r *Rollout) { r.CanaryPercent = -1 }},
		{"a canary reaching everybody", func(r *Rollout) { r.CanaryPercent = 100 }},
		{"a staged reaching everybody", func(r *Rollout) { r.StagedPercent = 100 }},
		{"a staged smaller than its canary", func(r *Rollout) { r.CanaryPercent, r.StagedPercent = 40, 20 }},
		{"a staged equal to its canary", func(r *Rollout) { r.CanaryPercent, r.StagedPercent = 20, 20 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.apply(&r)
			require.ErrorIs(t, ValidateRollout(r), ErrInvalidRollout)
		})
	}

	require.NoError(t, ValidateRollout(base()))
}

func TestRolloutSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	r := DefaultRollout(e.org, "disk-critical")
	r.CanaryPercent, r.StagedPercent = 5, 25
	r.UpdatedBy = "ivan"
	require.NoError(t, s.UpsertRollout(e.ctx, r))

	stored := mustListRollouts(t, s, e.ctx, e.org)["disk-critical"]
	assert.Equal(t, 5, stored.CanaryPercent)
	assert.Equal(t, 25, stored.StagedPercent)

	unconfigured := DefaultRollout(e.org, "cpu-saturated")
	assert.Equal(t, defaultCanaryPercent, unconfigured.CanaryPercent)
	assert.Equal(t, defaultStagedPercent, unconfigured.StagedPercent)
}

func TestStoppingARulePerCustomerAndTenantWide(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	other := seedSecondCustomer(t, e)

	require.NoError(t, s.StopRule(e.ctx, e.org, "disk-critical", "ivan"))
	assert.True(t, mustListRollouts(t, s, e.ctx, e.org)["disk-critical"].Kill)
	assert.Empty(t, mustListRollouts(t, s, e.ctx, other),
		"stopping one customer's rule must not touch another's")

	require.NoError(t, s.StopRuleTenantWide(e.ctx, "cpu-saturated", "ivan"))
	for _, org := range []uuid.UUID{e.org, other} {
		stopped := mustListRollouts(t, s, e.ctx, org)["cpu-saturated"]
		assert.True(t, stopped.Kill, "the tenant-wide stop reaches every customer")
		assert.False(t, stopped.Delivers())
	}

	assert.Empty(t, mustListRollouts(t, s, foreignTenant(t, e), e.org))
}

func TestResumingARuleClearsTheStopAndNothingElse(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	off := DefaultRollout(e.org, "disk-critical")
	off.Enabled = false
	require.NoError(t, s.UpsertRollout(e.ctx, off))
	require.NoError(t, s.StopRule(e.ctx, e.org, "disk-critical", "ivan"))
	require.NoError(t, s.ResumeRule(e.ctx, e.org, "disk-critical", "ivan"))

	resumed := mustListRollouts(t, s, e.ctx, e.org)["disk-critical"]
	assert.False(t, resumed.Kill, "the stop is lifted")
	assert.False(t, resumed.Enabled, "the customer's own choice is not overwritten by lifting a stop")
}

func seedSecondCustomer(t *testing.T, e estate) uuid.UUID {
	t.Helper()
	return testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
}

func foreignTenant(t *testing.T, e estate) context.Context {
	t.Helper()
	tenantID := uuid.New()
	testutil.EnsureTenant(t, context.Background(), e.store, tenantID, "Tenant "+tenantID.String()[:8])
	return dbtx.WithTenant(context.Background(), tenantID, false)
}

func TestResumingARuleTenantWideLiftsTheStopForEveryCustomer(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	other := seedSecondCustomer(t, e)

	require.NoError(t, s.StopRuleTenantWide(e.ctx, "cpu-saturated", "ivan"))
	for _, org := range []uuid.UUID{e.org, other} {
		require.True(t, mustListRollouts(t, s, e.ctx, org)["cpu-saturated"].Kill)
	}

	require.NoError(t, s.ResumeRuleTenantWide(e.ctx, "cpu-saturated", "ivan"))
	for _, org := range []uuid.UUID{e.org, other} {
		resumed := mustListRollouts(t, s, e.ctx, org)["cpu-saturated"]
		assert.False(t, resumed.Kill, "the tenant-wide stop is lifted for every customer")
		assert.True(t, resumed.Delivers(), "a rule with no stop on it delivers again")
	}

	assert.Empty(t, mustListRollouts(t, s, foreignTenant(t, e), e.org))
}

func TestResumingARuleTenantWideRefusesAnEmptyRuleID(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	assert.ErrorIs(t, s.ResumeRuleTenantWide(e.ctx, "", "ivan"), ErrInvalidRollout)
}
