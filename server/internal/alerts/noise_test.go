package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The count on a rule belongs to the customer whose estate it was taken from.
// Isolation stops it crossing a tenant; nothing in the database stops it
// crossing a customer, so that scoping is the query's own job — and a query
// missing it returns a plausible number belonging to somebody else.
func TestNoiseIsCountedPerCustomer(t *testing.T) {
	t.Parallel()

	e := newEstate(t)
	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	otherSite := testutil.SeedSiteIn(t, e.ctx, e.store, other)
	otherDevice := testutil.SeedDeviceIn(t, e.ctx, e.store, other, otherSite.ID)

	e.seedRuleAlerts(t, "disk-critical", e.org, e.device, 4, e.now.Add(-10*time.Minute))
	e.seedRuleAlerts(t, "disk-critical", other, otherDevice.ID, 40, e.now.Add(-10*time.Minute))

	mine, err := e.alerts.RuleNoise(e.ctx, e.org)
	require.NoError(t, err)
	assert.Equal(t, 4, mine["disk-critical"].Recent,
		"a customer's badge must not count another customer's alerts")

	theirs, err := e.alerts.RuleNoise(e.ctx, other)
	require.NoError(t, err)
	assert.Equal(t, 40, theirs["disk-critical"].Recent)
}

// The colour is relative to the rule's own usual rate, so a rule meant to be
// chatty does not sit permanently red.
func TestNoiseIsMeasuredAgainstTheRulesOwnHistory(t *testing.T) {
	t.Parallel()

	e := newEstate(t)

	// A chatty rule: ten an hour for the past week, ten in the last hour.
	e.seedRuleAlerts(t, "chatty", e.org, e.device, 10, e.now.Add(-10*time.Minute))
	e.seedRuleAlerts(t, "chatty", e.org, e.device, 1670, e.now.Add(-3*24*time.Hour))

	// A quiet rule with the same recent count and none of the history.
	e.seedRuleAlerts(t, "quiet", e.org, e.device, 10, e.now.Add(-10*time.Minute))
	e.seedRuleAlerts(t, "quiet", e.org, e.device, 167, e.now.Add(-3*24*time.Hour))

	noise, err := e.alerts.RuleNoise(e.ctx, e.org)
	require.NoError(t, err)

	assert.Equal(t, 10, noise["chatty"].Recent)
	assert.Equal(t, NoiseUsual, noise["chatty"].Level(),
		"ten an hour on a rule that always does ten an hour is not a problem")

	assert.Equal(t, 10, noise["quiet"].Recent)
	assert.Equal(t, NoiseHigh, noise["quiet"].Level(),
		"the same ten on a rule that does one an hour is what the badge is for")
}

// A rule with no history yet renders neutral rather than alarming. A fresh
// customer whose whole pack read red would be told nothing at all.
func TestARuleWithNoHistoryRendersNeutral(t *testing.T) {
	t.Parallel()

	e := newEstate(t)
	e.seedRuleAlerts(t, "brand-new", e.org, e.device, 3, e.now.Add(-5*time.Minute))

	noise, err := e.alerts.RuleNoise(e.ctx, e.org)
	require.NoError(t, err)

	got := noise["brand-new"]
	assert.Equal(t, 3, got.Recent)
	assert.False(t, got.HasHistory)
	assert.Equal(t, NoiseUnknown, got.Level())
}

// The levels themselves, stated without a database so each boundary is exact.
func TestNoiseLevels(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		noise Noise
		want  NoiseLevel
	}{
		{"nothing known yet", Noise{Recent: 9}, NoiseUnknown},
		{"quiet", Noise{Recent: 0, BaselinePerHour: 4, HasHistory: true}, NoiseQuiet},
		{"at its usual rate", Noise{Recent: 4, BaselinePerHour: 4, HasHistory: true}, NoiseUsual},
		{"half again its usual rate", Noise{Recent: 6, BaselinePerHour: 4, HasHistory: true}, NoiseUsual},
		{"twice its usual rate", Noise{Recent: 8, BaselinePerHour: 4, HasHistory: true}, NoiseElevated},
		{"four times its usual rate", Noise{Recent: 16, BaselinePerHour: 4, HasHistory: true}, NoiseHigh},
		{
			name:  "one firing on a rule that almost never fires",
			noise: Noise{Recent: 1, BaselinePerHour: 0.01, HasHistory: true},
			want:  NoiseUsual,
		},
		{
			name:  "a handful on a rule that almost never fires",
			noise: Noise{Recent: 8, BaselinePerHour: 0.01, HasHistory: true},
			want:  NoiseHigh,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.noise.Level())
		})
	}
}

// A read with no tenant on the context is refused rather than answered.
func TestNoiseRequiresTenantScope(t *testing.T) {
	t.Parallel()

	e := newEstate(t)
	_, err := e.alerts.RuleNoise(context.Background(), e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}

// seedRuleAlerts writes n alerts for one rule, received at receivedAt, spread
// across distinct windows so the identity constraint does not collapse them.
func (e estate) seedRuleAlerts(t *testing.T, ruleID string, org, device any, n int, receivedAt time.Time) {
	t.Helper()
	e.exec(t,
		`INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
		                     severity, window_start, window_end, observed_at, received_at)
		 SELECT gen_random_uuid(), $1, $2, $3, $6, 1, 'warning',
		        $4::timestamptz + make_interval(secs => g),
		        $4::timestamptz + make_interval(secs => g),
		        $4::timestamptz + make_interval(secs => g),
		        $4::timestamptz
		   FROM generate_series(1, $5) AS g`,
		e.tenant, org, device, receivedAt, n, ruleID)
}
