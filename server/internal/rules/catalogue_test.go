package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const validYAML = `
rules:
  - id: disk-critical
    version: 1
    severity: critical
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

	cat, err := loadFixture(t, strings.ReplaceAll(validYAML, "[device]", "[organization]"))
	require.NoError(t, err)
	def, ok := cat.Lookup("disk-critical")
	require.True(t, ok)
	assert.Equal(t, []string{"organization"}, def.GroupBy)
}

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
			name:    "unknown field",
			mutate:  func(y string) string { return y + "    thresold: 90\n" },
			wantErr: "field thresold not found",
		},
		{
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
			name: "tunable names a field that is not tunable",
			mutate: func(y string) string {
				return strings.ReplaceAll(y, "      threshold: {min: 50, max: 99}", "      metric: {min: 1, max: 2}")
			},
			wantErr: "tunable",
		},
		{
			name:    "an id with capitals",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "id: disk-critical", "id: Disk-Critical") },
			wantErr: "lower-case",
		},
		{
			name:    "no summary",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "    summary: A disk is nearly full.\n", "") },
			wantErr: "summary is required",
		},
		{
			name: "coverage names a reading the fleet does not collect",
			mutate: func(y string) string {
				return y + "    coverage_requires: [disk.spinning_rust]\n"
			},
			wantErr: "coverage_requires",
		},
		{
			name: "a further condition outside the vocabulary",
			mutate: func(y string) string {
				return y + "    all:\n      - metric: disk.spinning_rust\n        comparator: gt\n        threshold: 1\n        predicate: Instant\n"
			},
			wantErr: "term 0: metric",
		},
		{
			name:    "tunable bounds are inverted",
			mutate:  func(y string) string { return strings.ReplaceAll(y, "{min: 50, max: 99}", "{min: 99, max: 50}") },
			wantErr: "bounds",
		},
		{
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

func TestEmbeddedCatalogueLoadsAndIsImmutable(t *testing.T) {
	t.Parallel()

	cat, err := Embedded()
	require.NoError(t, err)
	assert.NotEmpty(t, cat.All(), "the shipped catalogue must contain rules")

	for _, def := range cat.All() {
		assert.NotEmpty(t, def.Summary, "%s must say what it is for", def.ID)
		assert.NotEmpty(t, def.GroupBy, "%s must say what its alerts are about", def.ID)
		assert.NotEmpty(t, def.Severity, "%s must say how bad it is", def.ID)
		if def.WatchesEvents() {
			assert.Empty(t, def.Metric, "%s watches words, so it names no reading", def.ID)
			continue
		}
		_, ok := protocol.CanonicalRuleMetric(def.Metric)
		assert.True(t, ok, "%s watches %s, which the fleet does not collect", def.ID, def.Metric)
	}
}

func TestLoadCatalogueRejectsAMutatedDefinitionForAnExistingVersion(t *testing.T) {
	t.Parallel()

	lock, err := DigestCatalogue([]byte(validYAML))
	require.NoError(t, err)

	_, err = LoadCatalogue([]byte(validYAML), lock)
	require.NoError(t, err)

	mutated := strings.ReplaceAll(validYAML, "threshold: 90", "threshold: 80")
	_, err = LoadCatalogue([]byte(mutated), lock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutab")

	bumped := strings.ReplaceAll(mutated, "version: 1", "version: 2")
	_, err = LoadCatalogue([]byte(bumped), lock)
	require.NoError(t, err)
}

func TestEmbeddedCatalogueMatchesItsCommittedLock(t *testing.T) {
	t.Parallel()

	require.NoError(t, VerifyEmbeddedLock(),
		"the embedded catalogue drifted from catalogue.lock; bump the rule's version and refresh the lock")
}
