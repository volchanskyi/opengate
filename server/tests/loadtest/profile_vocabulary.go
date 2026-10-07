package main

import (
	"fmt"
)

// Family is which question a run is asking.
type Family string

const (
	// FamilyNormal is the everyday shape: the load the system is expected to
	// carry, run to prove it still carries it.
	FamilyNormal Family = "normal"
	// FamilyPeak is the busiest ordinary hour.
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
	// FamilyScaling holds load and data constant and varies the resources.
	FamilyScaling Family = "scaling"
)

var families = []Family{
	FamilyNormal, FamilyPeak, FamilySpike, FamilySoak,
	FamilyBreakpoint, FamilyVolume, FamilyScaling,
}

// Families returns every family a profile may declare.
func Families() []Family { return append([]Family(nil), families...) }

// Environment is the class of system under test; the closed set excludes production.
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

// containsValue reports whether a vocabulary holds a value.
func containsValue[T comparable](vocabulary []T, value T) bool {
	for _, candidate := range vocabulary {
		if candidate == value {
			return true
		}
	}
	return false
}
