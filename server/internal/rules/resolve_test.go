package rules

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// contoso is one customer's estate: a tenant, the customer inside it, a site,
// and two machines with different jobs. The whole point of the ladder is that
// one estate can hold both without either needing its own rule.
type contoso struct {
	tenant  uuid.UUID
	org     uuid.UUID
	site    uuid.UUID
	fs01    Device // a file server; its disks run full by design
	dalWS12 Device // a workstation; a full disk there is a user's problem
	laptop  Device // neither tagged nor filed, so only the customer default reaches it
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

// The plan's own case: Contoso runs one disk-critical rule, its file servers
// want 95, DAL-WS-012 wants 90, and everything else takes the customer default.
// Each machine resolves to its own number, most-specific first.
func TestResolveTargetsMachinesByTagWithinOneCustomerRule(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()

	bindings := []Binding{
		orgBinding(c.org, def.ID, threshold(88)),
		targeted(orgBinding(c.org, def.ID, threshold(95)), Selector{"role": "file-server"}, 10),
		targeted(orgBinding(c.org, def.ID, threshold(90)), Selector{"role": "workstation"}, 10),
	}

	assert.Equal(t, 95.0, Resolve(def, c.fs01, bindings).Threshold)
	assert.Equal(t, 90.0, Resolve(def, c.dalWS12, bindings).Threshold)
	assert.InEpsilon(t, 88.0, Resolve(def, c.laptop, bindings).Threshold, 0.0001,
		"an untagged machine takes the customer default")
}

// A machine beats its site, a site beats its customer, a customer beats the
// tenant, and the tenant beats what shipped. The ordering itself lives in
// internal/settings; this proves the rule layer reads it rather than inventing
// a second one.
func TestResolveWalksTheTenancyLadderNarrowestFirst(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()

	at := func(level settings.Level, key uuid.UUID, value float64) Binding {
		return newBinding(c.org, def.ID, level, key, threshold(value))
	}

	tenantOnly := []Binding{at(settings.LevelTenant, c.tenant, 70)}
	assert.InEpsilon(t, 70.0, Resolve(def, c.fs01, tenantOnly).Threshold, 0.0001)

	withOrg := append(tenantOnly, at(settings.LevelOrganization, c.org, 75))
	assert.InEpsilon(t, 75.0, Resolve(def, c.fs01, withOrg).Threshold, 0.0001)

	withSite := append(withOrg, at(settings.LevelSite, c.site, 80))
	assert.InEpsilon(t, 80.0, Resolve(def, c.fs01, withSite).Threshold, 0.0001)

	withDevice := append(withSite, at(settings.LevelDevice, c.fs01.Scope.DeviceID, 85))
	assert.InEpsilon(t, 85.0, Resolve(def, c.fs01, withDevice).Threshold, 0.0001)

	// The unfiled laptop has no site rung, so the site binding simply does not
	// apply to it rather than failing.
	assert.InEpsilon(t, 75.0, Resolve(def, c.laptop, withSite).Threshold, 0.0001)
}

// Each parameter resolves on its own, so retuning a threshold on one machine
// does not silently drag the customer's sustain window down with it.
func TestResolveResolvesEachParameterIndependently(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()

	bindings := []Binding{
		orgBinding(c.org, def.ID, map[string]float64{"threshold": 85, "sustain_secs": 600}),
		newBinding(c.org, def.ID, settings.LevelDevice, c.fs01.Scope.DeviceID, threshold(95)),
	}

	got := Resolve(def, c.fs01, bindings)
	assert.InEpsilon(t, 95.0, got.Threshold, 0.0001, "the machine's own threshold wins")
	assert.Equal(t, uint32(600), got.SustainSecs, "the customer's sustain still applies")
	assert.InEpsilon(t, def.Clear, got.Clear, 0.0001, "what nobody set stays what shipped")
}

// Two tag selectors can match one machine. Which of them wins is stated by the
// operator through precedence, and never left to whatever order the rows came
// back in.
func TestResolveBreaksSelectorTiesByPrecedenceThenDeterministically(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()

	byRole := targeted(orgBinding(c.org, def.ID, threshold(95)), Selector{"role": "file-server"}, 10)
	byRole.ID = uuid.MustParse("00000000-0000-0000-0000-0000000000ff")
	byEnv := targeted(orgBinding(c.org, def.ID, threshold(60)), Selector{"env": "prod"}, 20)
	byEnv.ID = uuid.MustParse("00000000-0000-0000-0000-00000000000a")

	// The higher precedence wins whichever order the rows arrive in.
	assert.InEpsilon(t, 60.0, Resolve(def, c.fs01, []Binding{byRole, byEnv}).Threshold, 0.0001)
	assert.InEpsilon(t, 60.0, Resolve(def, c.fs01, []Binding{byEnv, byRole}).Threshold, 0.0001)

	// With precedence tied, resolution is still an answer rather than a coin
	// toss: the lowest binding id wins, in either row order. The database
	// refuses to store this pair at all, so it is a last-resort guarantee.
	byEnv.Precedence = 10
	first := Resolve(def, c.fs01, []Binding{byRole, byEnv}).Threshold
	second := Resolve(def, c.fs01, []Binding{byEnv, byRole}).Threshold
	assert.InEpsilon(t, first, second, 0.0001, "equal precedence must not depend on row order")
	assert.InEpsilon(t, 60.0, first, 0.0001, "the lowest binding id is the stated tie-break")
}

// A targeted binding is more specific than the level's blanket one, so it wins
// even when nobody set a precedence.
func TestResolvePrefersATargetedBindingOverTheLevelDefault(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()

	bindings := []Binding{
		orgBinding(c.org, def.ID, threshold(88)),
		targeted(orgBinding(c.org, def.ID, threshold(95)), Selector{"role": "file-server"}, 0),
	}
	assert.InEpsilon(t, 95.0, Resolve(def, c.fs01, bindings).Threshold, 0.0001)
	assert.InEpsilon(t, 88.0, Resolve(def, c.dalWS12, bindings).Threshold, 0.0001)
}

// Another customer's bindings are not this customer's, even inside one tenant —
// the case a tenant-scoped database read does not catch on its own.
func TestResolveIgnoresAnotherCustomersBindings(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	c := newContoso()
	fabrikam := uuid.New()

	bindings := []Binding{orgBinding(fabrikam, def.ID, threshold(55))}

	assert.InEpsilon(t, def.Threshold, Resolve(def, c.fs01, bindings).Threshold, 0.0001)
}

// The resolved rule is what actually goes on the wire, so its shape has to
// survive resolution intact.
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

// A rule pushed under a name from before the vitals rename still resolves to the
// dimension the fleet actually collects, so it keeps firing.
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

// The screen has to be able to say why a machine is at the number it is at, and
// name the tuned value that decided it — a rung, the labels it was aimed at, or
// the pack itself.
func TestWhatDecidedAMachinesNumber(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	org, site := uuid.New(), uuid.New()
	machine := Device{
		Scope: settings.Scope{DeviceID: uuid.New(), SiteID: site, OrganizationID: org},
		Tags:  map[string]string{"role": "file-server"},
	}

	aimed := targeted(orgBinding(org, def.ID, threshold(95)), Selector{"role": "file-server"}, 10)

	level, source := DecidedBy(def, machine, []Binding{aimed}, "threshold")
	assert.Equal(t, settings.LevelOrganization, level)
	assert.Equal(t, "set on this machine's customer, for machines labelled role=file-server", source)

	// A rung with nothing aimed at it says so without naming labels.
	atSite := newBinding(org, def.ID, settings.LevelSite, site, threshold(93))
	level, source = DecidedBy(def, machine, []Binding{aimed, atSite}, "threshold")
	assert.Equal(t, settings.LevelSite, level, "the narrower rung decides it")
	assert.Equal(t, "set on this machine's office", source)

	// A parameter nobody tuned falls to the pack, and so does one the rule does
	// not offer at all.
	level, source = DecidedBy(def, machine, []Binding{aimed}, "sustain_secs")
	assert.Equal(t, settings.LevelShipped, level)
	assert.Equal(t, "the value the rule ships", source)

	level, _ = DecidedBy(def, machine, []Binding{aimed}, "not_a_parameter")
	assert.Equal(t, settings.LevelShipped, level)
}

// Every rung is named the way a person reads it, so a screen never has to
// translate "organization" into "customer" for itself.
func TestEachRungIsNamedForAPerson(t *testing.T) {
	t.Parallel()

	for level, want := range map[settings.Level]string{
		settings.LevelDevice:       "machine",
		settings.LevelSite:         "office",
		settings.LevelOrganization: "customer",
		settings.LevelTenant:       "platform",
		settings.LevelShipped:      "shipped default",
	} {
		assert.Equal(t, want, levelWord(level))
	}
}

// A selector reads back as the labels it names, in a stable order.
func TestDescribingWhichMachinesAValueIsAimedAt(t *testing.T) {
	t.Parallel()

	assert.Empty(t, DescribeSelector(nil))
	assert.Equal(t, "role=file-server", DescribeSelector(Selector{"role": "file-server"}))
	assert.Equal(t, "env=production, role=file-server",
		DescribeSelector(Selector{"role": "file-server", "env": "production"}))
}
