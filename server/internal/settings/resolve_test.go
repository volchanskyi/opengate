package settings_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// contosoLadder is one machine's place in the tenancy ladder: DAL-WS-012, in
// Contoso's Dallas office, in the MSP's tenant.
func contosoLadder() settings.Scope {
	return settings.Scope{
		DeviceID:       uuid.MustParse("00000000-0000-0000-0000-0000000000d1"),
		SiteID:         uuid.MustParse("00000000-0000-0000-0000-0000000000e1"),
		OrganizationID: uuid.MustParse("00000000-0000-0000-0000-0000000000c1"),
		TenantID:       uuid.MustParse("00000000-0000-0000-0000-0000000000f1"),
	}
}

// TestNarrowestLevelWins is the resolution order N5 asks for, worked through the
// case the plan names: Dallas is all file servers and alarms at 95, but the one
// workstation in it alarms at 90.
func TestNarrowestLevelWins(t *testing.T) {
	t.Parallel()
	scope := contosoLadder()

	cases := []struct {
		name      string
		overrides []settings.Override[int]
		want      int
		wantLevel settings.Level
	}{
		{
			name: "the machine beats its site",
			overrides: []settings.Override[int]{
				{Level: settings.LevelSite, ScopeID: scope.SiteID, Value: 95},
				{Level: settings.LevelDevice, ScopeID: scope.DeviceID, Value: 90},
			},
			want:      90,
			wantLevel: settings.LevelDevice,
		},
		{
			name: "a file server in Dallas with nothing of its own takes the site's 95",
			overrides: []settings.Override[int]{
				{Level: settings.LevelSite, ScopeID: scope.SiteID, Value: 95},
			},
			want:      95,
			wantLevel: settings.LevelSite,
		},
		{
			name: "the site beats the customer",
			overrides: []settings.Override[int]{
				{Level: settings.LevelOrganization, ScopeID: scope.OrganizationID, Value: 85},
				{Level: settings.LevelSite, ScopeID: scope.SiteID, Value: 95},
			},
			want:      95,
			wantLevel: settings.LevelSite,
		},
		{
			name: "the customer beats the tenant default",
			overrides: []settings.Override[int]{
				{Level: settings.LevelTenant, ScopeID: scope.TenantID, Value: 80},
				{Level: settings.LevelOrganization, ScopeID: scope.OrganizationID, Value: 85},
			},
			want:      85,
			wantLevel: settings.LevelOrganization,
		},
		{
			name: "the tenant default beats what shipped",
			overrides: []settings.Override[int]{
				{Level: settings.LevelTenant, ScopeID: scope.TenantID, Value: 80},
			},
			want:      80,
			wantLevel: settings.LevelTenant,
		},
		{
			name:      "nothing set anywhere leaves what shipped",
			overrides: nil,
			want:      75,
			wantLevel: settings.LevelShipped,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, level := settings.Resolve(scope, tc.overrides, 75, settings.NarrowestWins)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantLevel, level)
		})
	}
}

// TestAnotherMachinesValueIsIgnored proves resolution is keyed on identity, not
// only on level: handing over a whole customer's overrides must not let the
// Austin office's number reach a machine in Dallas.
func TestAnotherMachinesValueIsIgnored(t *testing.T) {
	t.Parallel()
	scope := contosoLadder()

	got, level := settings.Resolve(scope, []settings.Override[int]{
		{Level: settings.LevelSite, ScopeID: uuid.New(), Value: 60},
		{Level: settings.LevelDevice, ScopeID: uuid.New(), Value: 50},
		{Level: settings.LevelOrganization, ScopeID: scope.OrganizationID, Value: 85},
	}, 75, settings.NarrowestWins)

	assert.Equal(t, 85, got, "only values set on this machine's own ladder apply")
	assert.Equal(t, settings.LevelOrganization, level)
}

// TestAnUnfiledMachineSkipsTheSiteRung covers the machine nobody has filed yet:
// the site rung is simply absent, and resolution continues to the customer
// rather than failing.
func TestAnUnfiledMachineSkipsTheSiteRung(t *testing.T) {
	t.Parallel()
	scope := contosoLadder()
	scope.SiteID = uuid.Nil

	got, level := settings.Resolve(scope, []settings.Override[int]{
		{Level: settings.LevelSite, ScopeID: uuid.Nil, Value: 95},
		{Level: settings.LevelOrganization, ScopeID: scope.OrganizationID, Value: 85},
	}, 75, settings.NarrowestWins)

	assert.Equal(t, 85, got, "an absent rung carries no value, even one keyed to the zero id")
	assert.Equal(t, settings.LevelOrganization, level)
}

// TestAStopBeatsEveryNarrowerLevel is the exception the option run surfaced. At
// 02:00 with a rule alarming on five thousand machines, a customer-wide stop
// must not be undone by a value someone set on one machine — so that class of
// setting reads the ladder the other way up.
func TestAStopBeatsEveryNarrowerLevel(t *testing.T) {
	t.Parallel()
	scope := contosoLadder()

	overrides := []settings.Override[bool]{
		{Level: settings.LevelDevice, ScopeID: scope.DeviceID, Value: true},
		{Level: settings.LevelOrganization, ScopeID: scope.OrganizationID, Value: false},
	}

	stopped, level := settings.Resolve(scope, overrides, true, settings.BroadestWins)
	assert.False(t, stopped, "the customer-wide stop wins")
	assert.Equal(t, settings.LevelOrganization, level)

	// The same values under the ordinary rule go the other way, which is exactly
	// why the two cannot share one direction.
	enabled, level := settings.Resolve(scope, overrides, true, settings.NarrowestWins)
	assert.True(t, enabled)
	assert.Equal(t, settings.LevelDevice, level)
}

// TestLevelNames keeps the level vocabulary printable, since a resolved value is
// only actionable alongside where it came from.
func TestLevelNames(t *testing.T) {
	t.Parallel()
	for level, want := range map[settings.Level]string{
		settings.LevelDevice:       "device",
		settings.LevelSite:         "site",
		settings.LevelOrganization: "organization",
		settings.LevelTenant:       "tenant",
		settings.LevelShipped:      "shipped",
	} {
		assert.Equal(t, want, level.String())
	}
	assert.Equal(t, "unknown", settings.Level(99).String())
}
