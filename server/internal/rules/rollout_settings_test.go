package rules

import (
	"context"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// An estate of twelve machines and an estate of five thousand do not want the
// same first stage, so the populations are the customer's to set — and the next
// evaluation reads them rather than the numbers the code was written with.
func TestARetunedPopulationSizesTheNextStage(t *testing.T) {
	t.Parallel()

	r := DefaultRollout(uuid.New(), "disk-critical")
	r.CanaryPercent, r.StagedPercent = 5, 25

	assert.Equal(t, 5, r.PercentForStage(StageCanary))
	assert.Equal(t, 25, r.PercentForStage(StageStaged))
	assert.Equal(t, 100, r.PercentForStage(StageFull))

	// The stage a stored reach puts the rule in is read against those same
	// numbers. Classifying 25 as a canary because the code ships a tenth would
	// hold a rule at a stage it has already left.
	r.RolloutPercent = 5
	assert.Equal(t, StageCanary, r.Stage())
	r.RolloutPercent = 25
	assert.Equal(t, StageStaged, r.Stage())
	r.RolloutPercent = 100
	assert.Equal(t, StageFull, r.Stage())

	// A rule mid-rollout still needs the estate counted to size its stage.
	assert.True(t, NeedsFleetSize(map[string]Rollout{"disk-critical": {
		Enabled: true, RolloutPercent: 25, CanaryPercent: 5, StagedPercent: 25,
	}}))
}

// The waiting period is the customer's too: an hour is the wrong hold for a rule
// whose symptom takes a working day to appear.
func TestARetunedWaitingPeriodHoldsTheStage(t *testing.T) {
	t.Parallel()

	now := time.Now()
	r := DefaultRollout(uuid.New(), "disk-critical")
	r.CanaryHold = 24 * time.Hour
	r.RolloutPercent = r.PercentForStage(StageCanary)
	r.StageEnteredAt = now.Add(-2 * time.Hour)

	held := DecideStage(r, GateReport{}, now)
	assert.Equal(t, StageHold, held.Action,
		"two quiet hours do not earn a stage whose hold is a day")

	r.StageEnteredAt = now.Add(-25 * time.Hour)
	advanced := DecideStage(r, GateReport{}, now)
	assert.Equal(t, StageAdvance, advanced.Action)
	assert.Equal(t, r.PercentForStage(StageStaged), advanced.Percent)
}

// The automatic pull-back is the mitigation for the one thing here that can
// degrade an estate at once, so it is not configuration. This asserts the
// absence: whatever a customer sets, a tripped gate still moves the rule back.
func TestTheAutomaticPullBackCannotBeSwitchedOff(t *testing.T) {
	t.Parallel()

	now := time.Now()
	dirty := []GateReport{
		{CeilingBreaches: 1},
		{ThrottleTrips: 1},
		{EvaluationErrors: 1},
	}

	for _, tuned := range everyRolloutPace(t) {
		for _, stage := range []Stage{StageCanary, StageStaged, StageFull} {
			r := tuned
			r.RolloutPercent = r.PercentForStage(stage)
			r.StageEnteredAt = now.Add(-time.Second)

			for _, report := range dirty {
				assertPullsBack(t, r, stage, report, now)
			}
		}
	}
}

// everyRolloutPace is the settings an operator could reach, spread across their
// whole allowed range — the point being that none of them changes the answer.
func everyRolloutPace(t *testing.T) []Rollout {
	t.Helper()
	var out []Rollout
	for _, canary := range []int{1, 5, 50} {
		for _, staged := range []int{60, 80, 99} {
			for _, hold := range []time.Duration{time.Minute, 30 * 24 * time.Hour} {
				r := DefaultRollout(uuid.New(), "disk-critical")
				r.CanaryPercent, r.StagedPercent = canary, staged
				r.CanaryHold, r.StagedHold = hold, hold
				require.NoError(t, ValidateRollout(r))
				out = append(out, r)
			}
		}
	}
	return out
}

// assertPullsBack states what a tripped gate must do, whatever it was tuned to.
func assertPullsBack(t *testing.T, r Rollout, stage Stage, report GateReport, now time.Time) {
	t.Helper()
	got := DecideStage(r, report, now)
	assert.NotEqualf(t, StageHold, got.Action,
		"a tripped gate at %s with canary %d%%, staged %d%% and a %s hold must never hold",
		stage, r.CanaryPercent, r.StagedPercent, r.CanaryHold)

	if stage == StageCanary {
		assert.Equal(t, StageHalt, got.Action, "the smallest stage has nowhere to fall back to")
		return
	}
	assert.Equal(t, StageRevert, got.Action)
	assert.Less(t, got.Percent, r.RolloutPercent,
		"a revert reaches fewer machines than the stage it left")
}

// The same absence, stated against the state itself: there is no field an
// operator could set to opt out of the pull-back, so no API can expose one.
func TestNoRolloutFieldCanOptOutOfThePullBack(t *testing.T) {
	t.Parallel()

	optOut := regexp.MustCompile(`(?i)revert|rollback|pull_?back|halt|auto`)
	for field := range reflect.TypeFor[Rollout]().Fields() {
		assert.NotRegexp(t, optOut, field.Name,
			"Rollout must carry nothing that reads as a switch over the automatic pull-back")
	}
}

// And against the storage, where a column added later would be the route a
// struct field never was.
func TestNoRolloutColumnCanOptOutOfThePullBack(t *testing.T) {
	t.Parallel()

	_, e := newEstate(t)
	rows, err := e.store.DB().QueryContext(e.ctx,
		`SELECT column_name FROM information_schema.columns
		  WHERE table_name = 'rule_rollout'
		    AND table_schema = current_schema()`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	optOut := regexp.MustCompile(`(?i)revert|rollback|pull_?back|auto`)
	var columns []string
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		assert.NotRegexp(t, optOut, column)
		columns = append(columns, column)
	}
	require.NoError(t, rows.Err())
	assert.Contains(t, columns, "canary_percent", "the settings that are configurable are still there")
}

// Populations and waiting periods are bounded on write, so a stage that reaches
// nobody or is held for a decade cannot be stored.
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
		{"a hold of seconds", func(r *Rollout) { r.CanaryHold = time.Second }},
		{"a hold of years", func(r *Rollout) { r.StagedHold = 365 * 24 * time.Hour }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.apply(&r)
			require.ErrorIs(t, ValidateRollout(r), ErrInvalidRollout)
		})
	}

	require.NoError(t, ValidateRollout(base()))
}

// Settings round-trip, and a customer who has set none is on the shipped pace
// rather than on zeros.
func TestRolloutSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	r := DefaultRollout(e.org, "disk-critical")
	r.CanaryPercent, r.StagedPercent = 5, 25
	r.CanaryHold, r.StagedHold = 2*time.Hour, 12*time.Hour
	r.UpdatedBy = "ivan"
	require.NoError(t, s.UpsertRollout(e.ctx, r))

	stored := mustListRollouts(t, s, e.ctx, e.org)["disk-critical"]
	assert.Equal(t, 5, stored.CanaryPercent)
	assert.Equal(t, 25, stored.StagedPercent)
	assert.Equal(t, 2*time.Hour, stored.CanaryHold)
	assert.Equal(t, 12*time.Hour, stored.StagedHold)

	unconfigured := DefaultRollout(e.org, "cpu-saturated")
	assert.Equal(t, defaultCanaryPercent, unconfigured.CanaryPercent)
	assert.Equal(t, defaultStagedPercent, unconfigured.StagedPercent)
	assert.Equal(t, defaultCanaryHold, unconfigured.CanaryHold)
	assert.Equal(t, defaultStagedHold, unconfigured.StagedHold)
}

// A stop reaches one customer and no other, and the tenant-wide one reaches
// every customer in the tenant and nobody outside it.
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

	// Nobody outside the tenant is touched, and nobody outside it can reach in.
	assert.Empty(t, mustListRollouts(t, s, foreignTenant(t, e), e.org))
}

// Resuming is the same action in reverse, and it is deliberately separate from
// the on/off toggle: a rule somebody stopped stays stopped until somebody says
// otherwise.
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

// seedSecondCustomer adds another customer inside the same tenant, which is what
// anything keyed on the customer has to be proven against.
func seedSecondCustomer(t *testing.T, e estate) uuid.UUID {
	t.Helper()
	return testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
}

// foreignTenant returns a context acting as a different tenant over the same
// database.
func foreignTenant(t *testing.T, e estate) context.Context {
	t.Helper()
	tenantID := uuid.New()
	testutil.EnsureTenant(t, context.Background(), e.store, tenantID, "Tenant "+tenantID.String()[:8])
	return dbtx.WithTenant(context.Background(), tenantID, false)
}
