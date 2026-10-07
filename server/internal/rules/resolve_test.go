package rules

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

type contoso struct {
	tenant  uuid.UUID
	org     uuid.UUID
	site    uuid.UUID
	fs01    Device
	dalWS12 Device
	laptop  Device
}

func newContoso() contoso {
	tenant, org, site := uuid.New(), uuid.New(), uuid.New()
	ladder := func(device uuid.UUID, inSite bool) settings.Scope {
		s := settings.Scope{DeviceID: device, OrganizationID: org, TenantID: tenant}
		if inSite {
			s.SiteID = site
		}
		return s
	}
	return contoso{
		tenant: tenant,
		org:    org,
		site:   site,
		fs01: Device{
			Scope: ladder(uuid.New(), true),
			Tags:  map[string]string{"role": "file-server", "env": "prod"},
		},
		dalWS12: Device{
			Scope: ladder(uuid.New(), true),
			Tags:  map[string]string{"role": "workstation", "env": "prod"},
		},
		laptop: Device{Scope: ladder(uuid.New(), false)},
	}
}

func assertThreshold(t *testing.T, want float64, def Definition, d Device, bindings []Binding, msg ...any) {
	t.Helper()
	assert.InEpsilon(t, want, Resolve(def, d, bindings).Threshold, 0.0001, msg...)
}

func TestResolveTargetsMachinesByTagWithinOneCustomerRule(t *testing.T) {
	t.Parallel()

	def, c := diskCritical(t), newContoso()

	bindings := []Binding{
		orgBinding(c.org, def.ID, threshold(88)),
		targeted(orgBinding(c.org, def.ID, threshold(95)), Selector{"role": "file-server"}, 10),
		targeted(orgBinding(c.org, def.ID, threshold(90)), Selector{"role": "workstation"}, 10),
	}

	assert.Equal(t, 95.0, Resolve(def, c.fs01, bindings).Threshold)
	assert.Equal(t, 90.0, Resolve(def, c.dalWS12, bindings).Threshold)
	assertThreshold(t, 88, def, c.laptop, bindings, "an untagged machine takes the customer default")
}

func TestResolveWalksTheTenancyLadderNarrowestFirst(t *testing.T) {
	t.Parallel()

	def, c := diskCritical(t), newContoso()

	at := func(level settings.Level, key uuid.UUID, value float64) Binding {
		return newBinding(c.org, def.ID, level, key, threshold(value))
	}

	tenantOnly := []Binding{at(settings.LevelTenant, c.tenant, 70)}
	assertThreshold(t, 70, def, c.fs01, tenantOnly)

	withOrg := append(tenantOnly, at(settings.LevelOrganization, c.org, 75))
	assertThreshold(t, 75, def, c.fs01, withOrg)

	withSite := append(withOrg, at(settings.LevelSite, c.site, 80))
	assertThreshold(t, 80, def, c.fs01, withSite)

	withDevice := append(withSite, at(settings.LevelDevice, c.fs01.Scope.DeviceID, 85))
	assertThreshold(t, 85, def, c.fs01, withDevice)

	assertThreshold(t, 75, def, c.laptop, withSite)
}

func TestResolveResolvesEachParameterIndependently(t *testing.T) {
	t.Parallel()

	def, c := diskCritical(t), newContoso()

	bindings := []Binding{
		orgBinding(c.org, def.ID, map[string]float64{"threshold": 85, "sustain_secs": 600}),
		newBinding(c.org, def.ID, settings.LevelDevice, c.fs01.Scope.DeviceID, threshold(95)),
	}

	got := Resolve(def, c.fs01, bindings)
	assert.InEpsilon(t, 95.0, got.Threshold, 0.0001, "the machine's own threshold wins")
	assert.Equal(t, uint32(600), got.SustainSecs, "the customer's sustain still applies")
	assert.InEpsilon(t, def.Clear, got.Clear, 0.0001, "what nobody set stays what shipped")
}

func TestResolveBreaksSelectorTiesByPrecedenceThenDeterministically(t *testing.T) {
	t.Parallel()

	def, c := diskCritical(t), newContoso()

	byRole := targeted(orgBinding(c.org, def.ID, threshold(95)), Selector{"role": "file-server"}, 10)
	byRole.ID = uuid.MustParse("00000000-0000-0000-0000-0000000000ff")
	byEnv := targeted(orgBinding(c.org, def.ID, threshold(60)), Selector{"env": "prod"}, 20)
	byEnv.ID = uuid.MustParse("00000000-0000-0000-0000-00000000000a")

	assertThreshold(t, 60, def, c.fs01, []Binding{byRole, byEnv})
	assertThreshold(t, 60, def, c.fs01, []Binding{byEnv, byRole})

	byEnv.Precedence = 10
	first := Resolve(def, c.fs01, []Binding{byRole, byEnv}).Threshold
	second := Resolve(def, c.fs01, []Binding{byEnv, byRole}).Threshold
	assert.InEpsilon(t, first, second, 0.0001, "equal precedence must not depend on row order")
	assert.InEpsilon(t, 60.0, first, 0.0001, "the lowest binding id is the stated tie-break")
}

func TestResolvePrefersATargetedBindingOverTheLevelDefault(t *testing.T) {
	t.Parallel()

	def, c := diskCritical(t), newContoso()

	bindings := []Binding{
		orgBinding(c.org, def.ID, threshold(88)),
		targeted(orgBinding(c.org, def.ID, threshold(95)), Selector{"role": "file-server"}, 0),
	}
	assertThreshold(t, 95, def, c.fs01, bindings)
	assertThreshold(t, 88, def, c.dalWS12, bindings)
}

func TestResolveIgnoresAnotherCustomersBindings(t *testing.T) {
	t.Parallel()

	def, c := diskCritical(t), newContoso()
	fabrikam := uuid.New()

	bindings := []Binding{orgBinding(fabrikam, def.ID, threshold(55))}

	assertThreshold(t, def.Threshold, def, c.fs01, bindings)
}

func TestResolveProducesTheWireRule(t *testing.T) {
	t.Parallel()

	def := shippedRule(t, "io-stalled")
	c := newContoso()
	got := Resolve(def, c.fs01, nil)

	assert.Equal(t, "io-stalled", got.ID)
	assert.Equal(t, "stall.io.some", got.Metric)
	assert.Equal(t, protocol.AlertComparatorGte, got.Comparator)
	assert.Equal(t, protocol.RulePredicateWindowMean, got.Predicate)
	assert.Equal(t, uint32(300), got.WindowSecs)
}

func TestResolveCanonicalizesALegacyMetricName(t *testing.T) {
	t.Parallel()

	legacy := Definition{
		ID: "legacy-mem", Version: 1, Summary: "memory",
		Metric: "mem.used", ComparatorName: "gte", Threshold: 95, Clear: 85,
		GroupBy: []string{"device"}, GroupWindowSecs: 300,
	}
	got := Resolve(legacy, newContoso().fs01, nil)
	assert.Equal(t, "mem.used_percent", got.Metric,
		"a rule written against the old name must reach the dimension that exists")
}

func TestWhatDecidedAMachinesNumber(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	org, site := uuid.New(), uuid.New()
	machine := Device{
		Scope: settings.Scope{DeviceID: uuid.New(), SiteID: site, OrganizationID: org},
		Tags:  map[string]string{"role": "file-server"},
	}

	aimed := targeted(orgBinding(org, def.ID, threshold(95)), Selector{"role": "file-server"}, 10)

	atSite := newBinding(org, def.ID, settings.LevelSite, site, threshold(93))
	decided := func(bindings []Binding, param string) (settings.Level, string) {
		return DecidedBy(def, machine, bindings, param)
	}

	level, source := decided([]Binding{aimed}, "threshold")
	assert.Equal(t, settings.LevelOrganization, level)
	assert.Equal(t, "set on this host's customer, for hosts labelled role=file-server", source)

	level, source = decided([]Binding{aimed, atSite}, "threshold")
	assert.Equal(t, settings.LevelSite, level, "the narrower rung decides it")
	assert.Equal(t, "set on this host's site", source)

	onHost := newBinding(org, def.ID, settings.LevelDevice, machine.Scope.DeviceID, threshold(91))
	level, source = decided([]Binding{aimed, atSite, onHost}, "threshold")
	assert.Equal(t, settings.LevelDevice, level)
	assert.Equal(t, "set on this host", source)

	level, source = decided([]Binding{aimed}, "sustain_secs")
	assert.Equal(t, settings.LevelShipped, level)
	assert.Equal(t, "the value the rule ships", source)

	level, _ = decided([]Binding{aimed}, "not_a_parameter")
	assert.Equal(t, settings.LevelShipped, level)
}

func TestEachRungIsNamedForAPerson(t *testing.T) {
	t.Parallel()

	for level, want := range map[settings.Level]string{
		settings.LevelDevice:       "host",
		settings.LevelSite:         "site",
		settings.LevelOrganization: "customer",
		settings.LevelTenant:       "platform",
		settings.LevelShipped:      "shipped default",
	} {
		assert.Equal(t, want, levelWord(level))
	}
}

func TestDescribingWhichMachinesAValueIsAimedAt(t *testing.T) {
	t.Parallel()

	assert.Empty(t, DescribeSelector(nil))
	assert.Equal(t, "role=file-server", DescribeSelector(Selector{"role": "file-server"}))
	assert.Equal(t, "env=production, role=file-server",
		DescribeSelector(Selector{"role": "file-server", "env": "production"}))
}
