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
	// Sessions is how many live remote sessions run concurrently. Like the
	// arrival rate above it is a technician-side number: opening a session is
	// the browser's side of the wire, so the machine-side harness carries this
	// into the run's evidence as an offer and never as an achievement.
	Sessions int `yaml:"sessions"`
}

// Safety is what makes a run stop itself. Nothing here is about the verdict.
type Safety struct {
	// MaxNodeCPUPercent is the promise made to whatever else sits on the node.
	// Staging shares one with production and declares it; a disposable stack
	// shares its node with nothing and leaves it out.
	MaxNodeCPUPercent float64 `yaml:"max_node_cpu_percent"`
	// MaxNodeMemoryPercent is the room the run can still exhaust. Past it the
	// node has nowhere to put what the run produces, so it is declared wherever
	// the run is.
	MaxNodeMemoryPercent float64 `yaml:"max_node_memory_percent"`
	// MaxErrorRate stops a run that has stopped measuring anything: past this,
	// the numbers describe the error path.
	MaxErrorRate float64 `yaml:"max_error_rate"`
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
	// Ungated names the measurements this profile has deliberately left without
	// a limit. A measurement that appears in neither list is one nobody has
	// ruled on, which is the state this pair exists to make visible.
	Ungated []Ungated `yaml:"ungated"`
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
	problems = append(problems, p.validateUngated()...)

	return errors.Join(problems...)
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
	problems = append(problems, p.validateProcessorCeiling()...)
	if p.Safety.MaxNodeMemoryPercent <= 0 || p.Safety.MaxNodeMemoryPercent > 100 {
		problems = append(problems, fmt.Errorf(
			"safety.max_node_memory_percent must be between 0 and 100, got %v", p.Safety.MaxNodeMemoryPercent))
	}
	if p.Safety.MaxErrorRate < 0 || p.Safety.MaxErrorRate > 1 {
		problems = append(problems, fmt.Errorf("safety.max_error_rate is a ratio between 0 and 1, got %v", p.Safety.MaxErrorRate))
	}
	return problems
}

// validateProcessorCeiling holds each environment to the ceiling that means
// something there. The processor ceiling is a promise made to whatever shares
// the node, so staging declares one and a disposable stack — created by the job
// and thrown away with it — may not: a ceiling nothing consults reads, to the
// next person, as protection that is not there.
func (p *Profile) validateProcessorCeiling() []error {
	if p.Environment == EnvRunner {
		if p.Safety.MaxNodeCPUPercent != 0 {
			return []error{fmt.Errorf(
				"safety.max_node_cpu_percent is declared as %v on a %q environment, which shares its node with nothing — leave it out",
				p.Safety.MaxNodeCPUPercent, EnvRunner)}
		}
		return nil
	}
	if p.Safety.MaxNodeCPUPercent <= 0 || p.Safety.MaxNodeCPUPercent > 100 {
		return []error{fmt.Errorf(
			"safety.max_node_cpu_percent must be between 0 and 100, got %v — the run shares its node with production",
			p.Safety.MaxNodeCPUPercent)}
	}
	return nil
}
