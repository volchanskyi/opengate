package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// A performance run is configured by exactly one file. Everything the run does
// — which fixture it builds on, which phases it walks, what it refuses to push
// past, and which numbers decide the verdict — is declared here rather than
// spread across a workflow, a script and a scenario, so two runs are comparable
// when their profiles are and not otherwise.
//
// The schema is versioned because a bundle records which version produced it. A
// trend that silently spans two meanings of the same field is worse than one
// with a gap in it.

// profileSchemaVersion is the schema this build understands. A profile written
// for another version is refused rather than half-read: the fields a reader
// does not know are the ones that changed what the run did.
const profileSchemaVersion = 1

// Family is which question a run is asking. The venue, the shape of the load
// and how the verdict reads all follow from it.
type Family string

const (
	// FamilyNormal is the everyday shape: the load the system is expected to
	// carry, run to prove it still carries it.
	FamilyNormal Family = "normal"
	// FamilyPeak is the busiest ordinary hour rather than an exceptional one.
	FamilyPeak Family = "peak"
	// FamilySpike is a step change with no ramp — a site coming back after an
	// outage, a rollout that restarts a fleet at once.
	FamilySpike Family = "spike"
	// FamilySoak holds a steady load long enough for what leaks to show.
	FamilySoak Family = "soak"
	// FamilyBreakpoint raises load until something gives, and reports what.
	FamilyBreakpoint Family = "breakpoint"
	// FamilyVolume holds load constant and varies how much data is already
	// there, which is the only way to separate the two.
	FamilyVolume Family = "volume"
	// FamilyScaling holds load and data constant and varies the resources, so
	// the answer is a shape rather than a single point.
	FamilyScaling Family = "scaling"
)

var families = []Family{
	FamilyNormal, FamilyPeak, FamilySpike, FamilySoak,
	FamilyBreakpoint, FamilyVolume, FamilyScaling,
}

// Families returns every family a profile may declare.
func Families() []Family { return append([]Family(nil), families...) }

// Environment is the class of system under test. Production is not a member,
// which is what makes "production is never a target" a property of the type
// rather than of a reviewer's attention.
type Environment string

const (
	// EnvStaging is the shared staging namespace.
	EnvStaging Environment = "staging"
	// EnvRunner is a disposable stack a CI runner brings up and throws away.
	EnvRunner Environment = "runner"
)

var environments = []Environment{EnvStaging, EnvRunner}

// Environments returns every environment class a profile may declare.
func Environments() []Environment { return append([]Environment(nil), environments...) }

// FixtureSize names one of the three committed fleet shapes.
type FixtureSize string

const (
	// FixtureSmall is the committed reference fleet.
	FixtureSmall FixtureSize = "small"
	// FixtureLarge is four times the reference fleet.
	FixtureLarge FixtureSize = "large"
	// FixtureLopsided is the large fleet with one customer holding most of it,
	// which is the shape a tenant-scoped read is actually asked to answer.
	FixtureLopsided FixtureSize = "lopsided"
)

var fixtureSizes = []FixtureSize{FixtureSmall, FixtureLarge, FixtureLopsided}

// FixtureSizes returns every fixture size a profile may declare.
func FixtureSizes() []FixtureSize { return append([]FixtureSize(nil), fixtureSizes...) }

// Duration is a YAML duration that insists on a unit. A bare number reads as
// seconds to one person and milliseconds to another, and a phase whose length
// depends on who wrote it is a run nobody can reproduce.
type Duration struct {
	time.Duration
}

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("duration must be a string like 30s or 2m: %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("duration %q must carry a unit (30s, 2m, 1h): %w", raw, err)
	}
	d.Duration = parsed
	return nil
}

// MarshalYAML writes the duration back in the form it was read.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// Phase is one segment of a run, held at a declared offered load.
type Phase struct {
	Name     string   `yaml:"name"`
	Duration Duration `yaml:"duration"`
	// OperatorArrivalsPerSecond is offered load, not achieved load. A run that
	// could not reach it records both, which is how a saturated generator is
	// told apart from a slow system.
	OperatorArrivalsPerSecond float64 `yaml:"operator_arrivals_per_second"`
	// ConnectedAgents is how many machines are held connected through the
	// phase, which is a level rather than a rate.
	ConnectedAgents int `yaml:"connected_agents"`
	// Sessions is how many live remote sessions run concurrently.
	Sessions int `yaml:"sessions"`
}

// Safety is what makes a run stop itself. Every ceiling here is about the
// machine the run shares with production, not about the verdict.
type Safety struct {
	MaxNodeCPUPercent    float64 `yaml:"max_node_cpu_percent"`
	MaxNodeMemoryPercent float64 `yaml:"max_node_memory_percent"`
	// MaxErrorRate stops a run that has stopped measuring anything: past this,
	// the numbers describe the error path.
	MaxErrorRate float64 `yaml:"max_error_rate"`
}

// Gate is one rule the bundle is read against. It never reads live state and
// never changes the workload.
type Gate struct {
	// Series is a source/scenario/phase triple, matching the trend rows.
	Series string `yaml:"series"`
	Metric string `yaml:"metric"`
	// Max and Min are pointers so "no ceiling" is distinguishable from zero.
	Max *float64 `yaml:"max"`
	Min *float64 `yaml:"min"`
	// Blocking says whether a breach fails the run or is reported as a
	// finding. A newly tightened mark stays advisory until fresh runs show the
	// spread it is measured against has narrowed.
	Blocking bool `yaml:"blocking"`
}

// Profile is one runnable configuration.
type Profile struct {
	SchemaVersion int         `yaml:"schema_version"`
	Name          string      `yaml:"name"`
	Family        Family      `yaml:"family"`
	Environment   Environment `yaml:"environment"`
	Fixture       FixtureSize `yaml:"fixture"`
	Phases        []Phase     `yaml:"phases"`
	Safety        Safety      `yaml:"safety"`
	Gates         []Gate      `yaml:"gates"`
}

// TotalDuration is how long the phases run for, end to end.
func (p *Profile) TotalDuration() time.Duration {
	var total time.Duration
	for _, phase := range p.Phases {
		total += phase.Duration.Duration
	}
	return total
}

// ParseProfile reads and validates one profile document.
func ParseProfile(data []byte) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// LoadProfile reads one profile from disk.
func LoadProfile(path string) (*Profile, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", path, err)
	}
	p, err := ParseProfile(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return p, nil
}

// LoadProfileDir reads every profile in a directory, in name order.
func LoadProfileDir(dir string) ([]*Profile, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	sort.Strings(matches)

	profiles := make([]*Profile, 0, len(matches))
	for _, path := range matches {
		p, err := LoadProfile(path)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// profileDir is the committed profile directory, resolved from this file's own
// location so it is found whatever the working directory is.
func profileDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "load", "profiles")
}

// Validate reports every reason this profile could not produce a comparable
// run. Errors are joined rather than returned one at a time, so editing a
// profile is one pass rather than a sequence of them.
func (p *Profile) Validate() error {
	var problems []error

	if p.SchemaVersion != profileSchemaVersion {
		problems = append(problems, fmt.Errorf("schema_version %d is not %d, which is the version this build reads",
			p.SchemaVersion, profileSchemaVersion))
	}
	if p.Name == "" {
		problems = append(problems, errors.New("name must be set — it is how a bundle says which profile produced it"))
	}
	problems = append(problems, p.validateVocabularies()...)
	problems = append(problems, p.validatePhases()...)
	problems = append(problems, p.validateSafety()...)
	problems = append(problems, p.validateGates()...)

	return errors.Join(problems...)
}

func (p *Profile) validateVocabularies() []error {
	var problems []error
	if !containsValue(families, p.Family) {
		problems = append(problems, fmt.Errorf("family %q is not one of %v", p.Family, families))
	}
	if !containsValue(environments, p.Environment) {
		problems = append(problems, fmt.Errorf("environment %q is not one of %v — production is not a target",
			p.Environment, environments))
	}
	if !containsValue(fixtureSizes, p.Fixture) {
		problems = append(problems, fmt.Errorf("fixture %q is not one of %v", p.Fixture, fixtureSizes))
	}
	return problems
}

func (p *Profile) validatePhases() []error {
	if len(p.Phases) == 0 {
		return []error{errors.New("a profile needs at least one phase")}
	}

	var problems []error
	seen := make(map[string]bool, len(p.Phases))
	for i, phase := range p.Phases {
		switch {
		case phase.Name == "":
			problems = append(problems, fmt.Errorf("phase %d has no name", i))
		case seen[phase.Name]:
			problems = append(problems, fmt.Errorf("duplicate phase name %q — a bundle keys its results by phase", phase.Name))
		default:
			seen[phase.Name] = true
		}
		if phase.Duration.Duration <= 0 {
			problems = append(problems, fmt.Errorf("phase %q has no duration", phase.Name))
		}
		if phase.OperatorArrivalsPerSecond < 0 {
			problems = append(problems, fmt.Errorf("phase %q: operator_arrivals_per_second cannot be negative", phase.Name))
		}
		if phase.ConnectedAgents < 0 {
			problems = append(problems, fmt.Errorf("phase %q: connected_agents cannot be negative", phase.Name))
		}
		if phase.Sessions < 0 {
			problems = append(problems, fmt.Errorf("phase %q: sessions cannot be negative", phase.Name))
		}
	}
	return problems
}

func (p *Profile) validateSafety() []error {
	var problems []error
	if p.Safety.MaxNodeCPUPercent <= 0 || p.Safety.MaxNodeCPUPercent > 100 {
		problems = append(problems, fmt.Errorf(
			"safety.max_node_cpu_percent must be between 0 and 100, got %v — the run shares its node with production",
			p.Safety.MaxNodeCPUPercent))
	}
	if p.Safety.MaxNodeMemoryPercent <= 0 || p.Safety.MaxNodeMemoryPercent > 100 {
		problems = append(problems, fmt.Errorf(
			"safety.max_node_memory_percent must be between 0 and 100, got %v", p.Safety.MaxNodeMemoryPercent))
	}
	if p.Safety.MaxErrorRate < 0 || p.Safety.MaxErrorRate > 1 {
		problems = append(problems, fmt.Errorf("safety.max_error_rate is a ratio between 0 and 1, got %v", p.Safety.MaxErrorRate))
	}
	return problems
}

func (p *Profile) validateGates() []error {
	var problems []error
	for i, gate := range p.Gates {
		if gate.Series == "" {
			problems = append(problems, fmt.Errorf("gate %d names no series", i))
		}
		if gate.Metric == "" {
			problems = append(problems, fmt.Errorf("gate %d names no metric", i))
		}
		if gate.Max == nil && gate.Min == nil {
			problems = append(problems, fmt.Errorf("gate %d (%s) declares neither max or min, so nothing can breach it",
				i, gate.Series))
		}
	}
	return problems
}

// containsValue reports whether a vocabulary holds a value.
func containsValue[T comparable](vocabulary []T, value T) bool {
	for _, candidate := range vocabulary {
		if candidate == value {
			return true
		}
	}
	return false
}
