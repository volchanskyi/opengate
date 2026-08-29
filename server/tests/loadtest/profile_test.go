package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A profile is the only thing a run is configured by, so what it refuses is as
// much a part of it as what it accepts. Everything below is a shape that would
// otherwise produce a run whose numbers cannot be compared with any other run's.

const minimalProfile = `
schema_version: 1
name: normal
family: normal
environment: staging
fixture: small
phases:
  - name: ramp
    duration: 30s
    operator_arrivals_per_second: 2
    connected_agents: 100
    sessions: 0
  - name: steady
    duration: 2m
    operator_arrivals_per_second: 5
    connected_agents: 500
    sessions: 5
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
gates:
  - series: k6/api-baseline/http
    metric: latency_p95_ms
    max: 100
    blocking: false
`

func TestProfileParsesEveryDeclaredField(t *testing.T) {
	p, err := ParseProfile([]byte(minimalProfile))
	require.NoError(t, err)

	assert.Equal(t, 1, p.SchemaVersion)
	assert.Equal(t, "normal", p.Name)
	assert.Equal(t, FamilyNormal, p.Family)
	assert.Equal(t, EnvStaging, p.Environment)
	assert.Equal(t, FixtureSmall, p.Fixture)

	require.Len(t, p.Phases, 2)
	assert.Equal(t, "ramp", p.Phases[0].Name)
	assert.Equal(t, 30*time.Second, p.Phases[0].Duration.Duration)
	assert.InDelta(t, 5.0, p.Phases[1].OperatorArrivalsPerSecond, 0)
	assert.Equal(t, 500, p.Phases[1].ConnectedAgents)
	assert.Equal(t, 5, p.Phases[1].Sessions)

	assert.InDelta(t, 85.0, p.Safety.MaxNodeCPUPercent, 0)
	assert.InDelta(t, 0.01, p.Safety.MaxErrorRate, 0)

	require.Len(t, p.Gates, 1)
	assert.Equal(t, "k6/api-baseline/http", p.Gates[0].Series)
	assert.False(t, p.Gates[0].Blocking, "a gate is advisory until it is declared blocking")
}

// The phase list is ordered and a run walks it in order, so the total is a
// property the profile can state about itself rather than something a reader
// adds up.
func TestProfileReportsItsOwnDuration(t *testing.T) {
	p, err := ParseProfile([]byte(minimalProfile))
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute+30*time.Second, p.TotalDuration())
}

func TestProfileRefusesWhatCannotBeCompared(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Profile)
		wantErr string
	}{
		{
			name:    "an unknown schema version",
			mutate:  func(p *Profile) { p.SchemaVersion = 2 },
			wantErr: "schema_version",
		},
		{
			name:    "no phases at all",
			mutate:  func(p *Profile) { p.Phases = nil },
			wantErr: "at least one phase",
		},
		{
			name:    "a phase with no name",
			mutate:  func(p *Profile) { p.Phases[0].Name = "" },
			wantErr: "name",
		},
		{
			name:    "two phases sharing a name",
			mutate:  func(p *Profile) { p.Phases[1].Name = p.Phases[0].Name },
			wantErr: "duplicate phase",
		},
		{
			name:    "a phase with no duration",
			mutate:  func(p *Profile) { p.Phases[0].Duration = Duration{} },
			wantErr: "duration",
		},
		{
			name:    "a negative arrival rate",
			mutate:  func(p *Profile) { p.Phases[0].OperatorArrivalsPerSecond = -1 },
			wantErr: "operator_arrivals_per_second",
		},
		{
			name:    "a negative agent count",
			mutate:  func(p *Profile) { p.Phases[0].ConnectedAgents = -1 },
			wantErr: "connected_agents",
		},
		{
			name:    "an unknown family",
			mutate:  func(p *Profile) { p.Family = "vibes" },
			wantErr: "family",
		},
		{
			name:    "an unknown environment class",
			mutate:  func(p *Profile) { p.Environment = "production" },
			wantErr: "environment",
		},
		{
			name:    "an unknown fixture size",
			mutate:  func(p *Profile) { p.Fixture = "enormous" },
			wantErr: "fixture",
		},
		{
			name:    "no safety ceiling on processor use",
			mutate:  func(p *Profile) { p.Safety.MaxNodeCPUPercent = 0 },
			wantErr: "max_node_cpu_percent",
		},
		{
			name:    "no safety ceiling on memory",
			mutate:  func(p *Profile) { p.Safety.MaxNodeMemoryPercent = 0 },
			wantErr: "max_node_memory_percent",
		},
		{
			name:    "a gate naming no series",
			mutate:  func(p *Profile) { p.Gates[0].Series = "" },
			wantErr: "series",
		},
		{
			name:    "a gate with neither a ceiling nor a floor",
			mutate:  func(p *Profile) { p.Gates[0].Max = nil; p.Gates[0].Min = nil },
			wantErr: "max or min",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseProfile([]byte(minimalProfile))
			require.NoError(t, err)
			tc.mutate(p)
			err = p.Validate()
			require.Error(t, err, "profile must refuse %s", tc.name)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Production is never a target, and a profile is a file somebody edits. The
// refusal has to be in the type rather than in a reviewer's attention.
func TestProfileNeverAcceptsProductionAsAnEnvironment(t *testing.T) {
	_, err := ParseProfile([]byte(`
schema_version: 1
name: sneaky
family: normal
environment: production
fixture: small
phases:
  - {name: steady, duration: 1m, connected_agents: 1}
safety: {max_node_cpu_percent: 85, max_node_memory_percent: 90}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment")
}

// Malformed YAML must name itself rather than silently producing a zero
// profile that then fails validation for the wrong reason.
func TestProfileRejectsMalformedYAML(t *testing.T) {
	_, err := ParseProfile([]byte("phases: [oh no"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse profile")
}

// A duration written the way a person writes one. Bare seconds are ambiguous
// between seconds and milliseconds, and a profile that reads either way is a
// run nobody can reproduce.
func TestDurationRequiresAUnit(t *testing.T) {
	var d Duration
	require.Error(t, d.UnmarshalYAML(yamlNode(t, "300")))
	require.NoError(t, d.UnmarshalYAML(yamlNode(t, "300ms")))
	assert.Equal(t, 300*time.Millisecond, d.Duration)
}

// Every committed profile is loadable and valid. A profile that only exists to
// be read by a workflow is exactly the file that rots unnoticed.
func TestCommittedProfilesAreValid(t *testing.T) {
	profiles, err := LoadProfileDir(profileDir())
	require.NoError(t, err)
	require.NotEmpty(t, profiles, "no profiles found under load/profiles")

	seen := map[string]string{}
	for _, p := range profiles {
		assert.NoError(t, p.Validate(), "profile %s", p.Name)
		if other, dup := seen[p.Name]; dup {
			t.Errorf("profile name %q is used by both %s and %s", p.Name, other, p.Name)
		}
		seen[p.Name] = p.Name
	}
}

// Every family the strategy names has a profile, so a family cannot be
// described in the documentation and be unrunnable.
func TestEveryFamilyHasAProfile(t *testing.T) {
	profiles, err := LoadProfileDir(profileDir())
	require.NoError(t, err)

	have := map[Family]bool{}
	for _, p := range profiles {
		have[p.Family] = true
	}
	for _, family := range Families() {
		assert.True(t, have[family], "no profile declares family %q", family)
	}
}

// yamlNode builds a scalar YAML node, so a scalar decoder can be tested
// without a document around it.
func yamlNode(t *testing.T, value string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(value), &doc))
	require.NotEmpty(t, doc.Content)
	return doc.Content[0]
}

// The processor ceiling is a promise made to a neighbour, and a disposable stack
// has none. Declaring one there writes down a number nothing consults — and a
// ceiling nothing consults reads, to the next person, as protection that is not
// there. So the profile is refused rather than quietly ignored.
func TestADisposableStackMayNotDeclareAProcessorCeiling(t *testing.T) {
	_, err := ParseProfile([]byte(`
schema_version: 1
name: scaling
family: scaling
environment: runner
fixture: small
phases:
  - {name: steady, duration: 1m, connected_agents: 1}
safety: {max_node_cpu_percent: 95, max_node_memory_percent: 90}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_node_cpu_percent")
	assert.Contains(t, err.Error(), "runner")
}

// The room it can still run out of is declared all the same: past the memory
// ceiling the node has nowhere to put what the run produces, wherever the run is.
func TestADisposableStackDeclaresTheRoomItCanRunOutOf(t *testing.T) {
	p, err := ParseProfile([]byte(`
schema_version: 1
name: scaling
family: scaling
environment: runner
fixture: small
phases:
  - {name: steady, duration: 1m, connected_agents: 1}
safety: {max_node_memory_percent: 90, max_error_rate: 0.01}
`))
	require.NoError(t, err)
	assert.InDelta(t, 0.0, p.Safety.MaxNodeCPUPercent, 0, "no ceiling is declared where none applies")
	assert.InDelta(t, 90.0, p.Safety.MaxNodeMemoryPercent, 0)
}
