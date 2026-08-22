package main

import (
	"fmt"
	"sort"
)

// A run has three outcomes, not two.
//
// Valid and failed are both measurements: one of a system that held, one of a
// system that did not. Invalid is the third, and it is the one that was
// missing — the run did not measure the system at all, because the generator
// had nothing left, a safety ceiling stopped it, or a scenario produced no rows.
//
// Keeping it separate is the whole point. A night where one half ran and the
// other produced nothing, absorbed as data, pulls the window median down; the
// next genuinely slow night is then compared against that lowered median and
// passes. One partial night quietly costs two.

// Result is a run's outcome.
type Result string

const (
	// ResultValid is a run that measured the system and cleared its gates.
	ResultValid Result = "valid"
	// ResultFailed is a run that measured the system and breached a gate. It
	// stays in the trend: a slow night is exactly what the trend is for.
	ResultFailed Result = "failed"
	// ResultInvalid is a run that did not measure the system. It never enters
	// the trend.
	ResultInvalid Result = "invalid"
)

// Generator-saturation thresholds. Past either of these the run is measuring
// the generator rather than the target, and no amount of care about the target
// makes the number mean anything.
const (
	minGeneratorCPUHeadroomPercent = 20.0
	maxGeneratorMemoryUsedPercent  = 90.0
)

// defaultMaxErrorRate is the ceiling used when a run classifies without a
// profile. A missing ceiling must not read as an unlimited one.
const defaultMaxErrorRate = 0.25

// minAchievedFraction is how much of the offered arrival rate has to have
// arrived for the phase to be a measurement of the target. Below it, the
// generator never asked the questions the numbers are answers to.
const minAchievedFraction = 0.8

// Verdict is the classification plus everything a reader needs to see why.
type Verdict struct {
	Result  Result   `json:"result"`
	Reasons []string `json:"reasons,omitempty"`

	// The completeness record. Naming both halves means a reader sees what ran
	// rather than inferring it from which rows happened to arrive.
	ProducedScenarios   []string `json:"produced_scenarios,omitempty"`
	MissingScenarios    []string `json:"missing_scenarios,omitempty"`
	UnexpectedScenarios []string `json:"unexpected_scenarios,omitempty"`
}

// EntersTrend reports whether this run's rows may be stored. Only a run that
// measured the system may move a window median.
func (v Verdict) EntersTrend() bool { return v.Result != ResultInvalid }

// RunInputs is everything the classification reads. It reads no live state:
// the same inputs always produce the same verdict, which is what lets a verdict
// be recomputed from a stored bundle years later.
type RunInputs struct {
	Profile *Profile

	// ExpectedScenarios is what the run was supposed to produce rows for;
	// ProducedScenarios is what actually did.
	ExpectedScenarios []string
	ProducedScenarios []string

	Headroom Headroom
	Phases   []PhaseResult

	// SafetyBreaches are ceilings the run crossed and stopped for.
	SafetyBreaches []string
	// GateBreaches are gate rules the results broke. These are findings about
	// the system, so they fail the run rather than invalidating it.
	GateBreaches []string
}

// Classify decides a run's outcome.
func Classify(in RunInputs) Verdict {
	verdict := Verdict{ProducedScenarios: sortedCopy(in.ProducedScenarios)}
	verdict.MissingScenarios, verdict.UnexpectedScenarios = scenarioDifference(in.ExpectedScenarios, in.ProducedScenarios)

	invalidating := invalidReasons(in, verdict)
	if len(invalidating) > 0 {
		verdict.Result = ResultInvalid
		verdict.Reasons = invalidating
		return verdict
	}

	if len(in.GateBreaches) > 0 {
		verdict.Result = ResultFailed
		verdict.Reasons = append([]string(nil), in.GateBreaches...)
		return verdict
	}

	verdict.Result = ResultValid
	return verdict
}

// invalidReasons collects every reason this run measured something other than
// the system under test. All of them are collected rather than the first one
// returned, so one look at a bundle says everything that went wrong.
func invalidReasons(in RunInputs, verdict Verdict) []string {
	var reasons []string

	for _, scenario := range verdict.MissingScenarios {
		reasons = append(reasons, fmt.Sprintf(
			"scenario %q produced no rows, so this run is a partial night rather than a measurement", scenario))
	}
	for _, scenario := range verdict.UnexpectedScenarios {
		reasons = append(reasons, fmt.Sprintf(
			"scenario %q produced rows the profile never asked for", scenario))
	}

	if in.Headroom.CPUHeadroomPercent < minGeneratorCPUHeadroomPercent {
		reasons = append(reasons, fmt.Sprintf(
			"generator had %.1f%% processor headroom (floor %.0f%%), so the run measured the generator",
			in.Headroom.CPUHeadroomPercent, minGeneratorCPUHeadroomPercent))
	}
	if in.Headroom.MemoryUsedPercent > maxGeneratorMemoryUsedPercent {
		reasons = append(reasons, fmt.Sprintf(
			"generator memory reached %.1f%% (ceiling %.0f%%), so the run measured the generator",
			in.Headroom.MemoryUsedPercent, maxGeneratorMemoryUsedPercent))
	}

	for _, breach := range in.SafetyBreaches {
		reasons = append(reasons, "safety limit breached: "+breach)
	}

	reasons = append(reasons, phaseReasons(in)...)
	return reasons
}

func phaseReasons(in RunInputs) []string {
	maxErrorRate := defaultMaxErrorRate
	if in.Profile != nil && in.Profile.Safety.MaxErrorRate > 0 {
		maxErrorRate = in.Profile.Safety.MaxErrorRate
	}

	var reasons []string
	for _, phase := range in.Phases {
		if phase.ErrorRate > maxErrorRate {
			reasons = append(reasons, fmt.Sprintf(
				"phase %q error rate %.3f is past the ceiling %.3f, so its numbers describe the error path",
				phase.Name, phase.ErrorRate, maxErrorRate))
		}
		if fraction := phase.AchievedFraction(); fraction < minAchievedFraction {
			reasons = append(reasons, fmt.Sprintf(
				"phase %q reached %.0f%% of the offered arrival rate (floor %.0f%%), so the load was never offered",
				phase.Name, fraction*100, minAchievedFraction*100))
		}
	}
	return reasons
}

// scenarioDifference reports which expected scenarios produced nothing and
// which unexpected ones produced rows.
func scenarioDifference(expected, produced []string) (missing, unexpected []string) {
	producedSet := make(map[string]bool, len(produced))
	for _, name := range produced {
		producedSet[name] = true
	}
	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
		if !producedSet[name] {
			missing = append(missing, name)
		}
	}
	for _, name := range produced {
		if !expectedSet[name] {
			unexpected = append(unexpected, name)
		}
	}
	return missing, unexpected
}

// sortedCopy returns a sorted copy, so a verdict reads the same whatever order
// the scenarios finished in.
func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
