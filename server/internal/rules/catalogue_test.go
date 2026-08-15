package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// validYAML is a minimal well-formed one-rule catalogue. Every rejection test
// below starts from this and breaks exactly one thing, so a failure names the
// field that broke rather than "the fixture is wrong".
const validYAML = `
rules:
  - id: disk-critical
    version: 1
    summary: A disk is nearly full.
    metric: disk.used_percent
    comparator: gte
    threshold: 90
    clear: 85
    sustain_secs: 300
    predicate: Instant
    group_by: [device]
    group_window_secs: 300
    evidence: [vitals, top_processes]
    tunable:
      threshold: {min: 50, max: 99}
      clear: {min: 40, max: 98}
`

// loadFixture parses YAML with immutability checking disabled, which is what
// every test that is not about the lock wants.
func loadFixture(t *testing.T, yaml string) (*Catalogue, error) {
	t.Helper()
	return LoadCatalogue([]byte(yaml), nil)
}

func TestLoadCatalogueAcceptsAWellFormedRule(t *testing.T) {
	t.Parallel()

	cat, err := loadFixture(t, validYAML)
	require.NoError(t, err)
	require.Len(t, cat.All(), 1)

	def, ok := cat.Lookup("disk-critical")
	require.True(t, ok)
	assert.Equal(t, "disk-critical", def.ID)
	assert.Equal(t, 1, def.Version)
	assert.Equal(t, "disk.used_percent", def.Metric)
	assert.Equal(t, protocol.AlertComparatorGte, def.Comparator())
	assert.Equal(t, protocol.RulePredicateInstant, def.Predicate())
	assert.Equal(t, []string{"device"}, def.GroupBy)
}

// TestNoRuleMayGroupAboveTheCustomer pins the ceiling on grouping. The customer
// is the widest a room may be: at the tenant, Contoso's driver rollout and
// Fabrikam's unrelated outage land in one incident with no correct assignee, and
// the MSP's technician opens a room about two estates. Nothing in the grammar
// spells `tenant` today, and this is what keeps it that way — an
// unreachable-by-convention rule is exactly how a ceiling comes back.
func TestNoRuleMayGroupAboveTheCustomer(t *testing.T) {
	t.Parallel()

	for _, above := range []string{"tenant", "fleet", "msp", "global"} {
		t.Run(above, func(t *testing.T) {
			t.Parallel()
			_, err := loadFixture(t, strings.ReplaceAll(validYAML, "[device]", "["+above+"]"))
			require.Error(t, err, "grouping never crosses a customer boundary")
			assert.Contains(t, err.Error(), "group_by")
		})
	}

	// The customer itself is the widest that is allowed, so the refusal above is
	// a ceiling rather than a vocabulary that happens to be short.
	cat, err := loadFixture(t, strings.ReplaceAll(validYAML, "[device]", "[organization]"))
	require.NoError(t, err)
	def, ok := cat.Lookup("disk-critical")
	require.True(t, ok)
	assert.Equal(t, []string{"organization"}, def.GroupBy)
}

// A rule is only ever addressed by its id, so two definitions sharing one is
// ambiguous rather than additive.
func TestLoadCatalogueRejectsADuplicateRuleVersion(t *testing.T) {
	t.Parallel()

	_, err := loadFixture(t, validYAML+strings.TrimPrefix(validYAML, "\nrules:"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestLoadCatalogueRejectsMalformedDefinitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{
			// A field the loader does not know is a typo or a feature that does
			// not exist; either way silently ignoring it ships a rule that does
			// not do what it says.
			name:    "unknown field",
			mutate:  func(y string) string { return y + "    thresold: 90\n" },
			wantErr: "field thresold not found",
		},
		{
			// Without a grouping key a rule cannot say what its alerts are about,
			// so there is nothing to correlate or de-duplicate them by.
			name:    "missing group_by",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "    group_by: [device]\n", "") },
			wantErr: "group_by",
		},
		{
			name:    "empty group_by",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "[device]", "[]") },
			wantErr: "group_by",
		},
		{
			name:    "group_by outside the vocabulary",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "[device]", "[hostname]") },
			wantErr: "group_by",
		},
		{
			// A metric the fleet does not collect can never fire, so a rule
			// naming one is dead on arrival rather than merely quiet.
			name:    "metric outside the vocabulary",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "disk.used_percent", "disk.spinning_rust") },
			wantErr: "metric",
		},
		{
			name:    "comparator outside the vocabulary",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "comparator: gte", "comparator: approximately") },
			wantErr: "comparator",
		},
		{
			name:    "predicate outside the grammar",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "predicate: Instant", "predicate: Fourier") },
			wantErr: "predicate",
		},
		{
			name:    "empty id",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "id: disk-critical", `id: ""`) },
			wantErr: "id is required",
		},
		{
			name:    "version below one",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "version: 1", "version: 0") },
			wantErr: "version",
		},
		{
			name:    "group_window_secs of zero",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "group_window_secs: 300", "group_window_secs: 0") },
			wantErr: "group_window_secs",
		},
		{
			name:    "evidence outside the vocabulary",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "[vitals, top_processes]", "[core_dump]") },
			wantErr: "evidence",
		},
		{
			// Only the fields the grammar can actually carry are tunable; a
			// binding naming anything else would never reach the agent.
			name: "tunable names a field that is not tunable",
			mutate: func(y string) string {
				return strings.ReplaceAll(y, "      threshold: {min: 50, max: 99}", "      metric: {min: 1, max: 2}")
			},
			wantErr: "tunable",
		},
		{
			name:    "tunable bounds are inverted",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "{min: 50, max: 99}", "{min: 99, max: 50}") },
			wantErr: "bounds",
		},
		{
			// A rule shipping a value its own bindings would be refused is a
			// contradiction the catalogue must not be able to state.
			name:    "shipped default outside its own declared bounds",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "{min: 50, max: 99}", "{min: 95, max: 99}") },
			wantErr: "outside",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadFixture(t, tc.mutate(validYAML))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// The catalogue is the one place a rule's shape is decided, so it must be
// loadable — a typo here is a build-time failure, never a runtime surprise.
func TestEmbeddedCatalogueLoadsAndIsImmutable(t *testing.T) {
	t.Parallel()

	cat, err := Embedded()
	require.NoError(t, err)
	assert.NotEmpty(t, cat.All(), "the shipped catalogue must contain rules")

	for _, def := range cat.All() {
		assert.NotEmpty(t, def.Summary, "%s must say what it is for", def.ID)
		assert.NotEmpty(t, def.GroupBy, "%s must say what its alerts are about", def.ID)
		_, ok := protocol.CanonicalRuleMetric(def.Metric)
		assert.True(t, ok, "%s watches %s, which the fleet does not collect", def.ID, def.Metric)
	}
}

// Immutability per (rule_id, version) is the assertion the lock file exists to
// make: editing a shipped definition without bumping its version is refused at
// load, so a rule cannot change meaning underneath an alert already raised by it.
func TestLoadCatalogueRejectsAMutatedDefinitionForAnExistingVersion(t *testing.T) {
	t.Parallel()

	lock, err := DigestCatalogue([]byte(validYAML))
	require.NoError(t, err)

	// The same bytes still load: the digest is over what the rule means.
	_, err = LoadCatalogue([]byte(validYAML), lock)
	require.NoError(t, err)

	mutated := strings.ReplaceAll(validYAML, "threshold: 90", "threshold: 80")
	_, err = LoadCatalogue([]byte(mutated), lock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutab")

	// Bumping the version is how a definition is allowed to change.
	bumped := strings.ReplaceAll(mutated, "version: 1", "version: 2")
	_, err = LoadCatalogue([]byte(bumped), lock)
	require.NoError(t, err)
}

// The shipped catalogue is locked against its own committed digests, so the
// gate is live rather than merely available.
func TestEmbeddedCatalogueMatchesItsCommittedLock(t *testing.T) {
	t.Parallel()

	require.NoError(t, VerifyEmbeddedLock(),
		"the embedded catalogue drifted from catalogue.lock; bump the rule's version and refresh the lock")
}
