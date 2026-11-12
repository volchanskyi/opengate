package main

import (
	"fmt"
	"sort"
)

// Result is a run's outcome.
type Result string

const (
	// ResultValid is a run that measured the system and cleared its gates.
	ResultValid Result = "valid"
	// ResultFailed is a run that measured the system and breached a gate; it stays in the trend.
	ResultFailed Result = "failed"
	// ResultInvalid is a run that did not measure the system; it never enters the trend.
	ResultInvalid Result = "invalid"
)

// Generator-saturation thresholds; past any of them the run measures the generator.
// They read the generator's own allowance because a shared box's headroom belongs to both ends.
const (
	minGeneratorCPUHeadroomPercent = 20.0
	maxGeneratorMemoryUsedPercent  = 90.0
	// maxGeneratorCPURefusedPercent is the share of a run the generator may spend runnable
	// and denied the processor; past it that wait sits inside every round trip it timed.
	maxGeneratorCPURefusedPercent = 20.0
)

// defaultMaxErrorRate is the ceiling used when a run classifies without a profile.
const defaultMaxErrorRate = 0.25

// maxGeneratorLateShare is the share of a phase's dials the generator may begin past their turn;
// past it the phase's arrivals follow the generator's own schedule.
const maxGeneratorLateShare = 0.01

// maxRetainedGoroutinesPerOperation is the goroutines one completed operation may leave behind.
// Resident memory is recorded but ungated, because the Go runtime returns freed arena slowly.
const maxRetainedGoroutinesPerOperation = 0.5

// Verdict is the classification plus everything a reader needs to see why.
type Verdict struct {
	Result  Result   `json:"result"`
	Reasons []string `json:"reasons,omitempty"`

	// The completeness record: which scenarios ran, which are missing and which were unexpected.
	ProducedScenarios   []string `json:"produced_scenarios,omitempty"`
	MissingScenarios    []string `json:"missing_scenarios,omitempty"`
	UnexpectedScenarios []string `json:"unexpected_scenarios,omitempty"`
}

// EntersTrend reports whether this run's rows may be stored; only a measuring run moves a median.
func (v Verdict) EntersTrend() bool { return v.Result != ResultInvalid }

// RunInputs is everything the classification reads, so a stored bundle reproduces its verdict.
type RunInputs struct {
	Profile *Profile

	// ExpectedScenarios is what the run should produce rows for; ProducedScenarios is what did.
	ExpectedScenarios []string
	ProducedScenarios []string

	Headroom Headroom
	// Technicians is the technicians' own allowance, judged by the generator's ceilings; nil where
	// they share the generator's.
	Technicians *Headroom
	Phases      []PhaseResult

	// Target is the target's state before and after the run and the work between; zero means unread.
	Target TargetConservation

	// BreakingPoint is the ladder's answer; rungs at or above the one that gave out are exempt.
	BreakingPoint *BreakingPoint

	// Helper is what the run's sessions asked the server's helper and what it answered.
	Helper *HelperCount

	// SafetyBreaches are ceilings the run crossed and stopped for.
	SafetyBreaches []string
	// GateBreaches are gate rules the results broke; they fail the run.
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

	breaches := append([]string(nil), in.GateBreaches...)
	breaches = append(breaches, conservationBreaches(in.Target)...)
	breaches = append(breaches, helperBreaches(in.Helper)...)
	if len(breaches) > 0 {
		verdict.Result = ResultFailed
		verdict.Reasons = breaches
		return verdict
	}

	verdict.Result = ResultValid
	return verdict
}

// helperBreaches fails a run whose sessions asked the helper something it did not answer: a
// session's attempt gathers its addresses from those answers.
func helperBreaches(helper *HelperCount) []string {
	if helper == nil || helper.Answered >= helper.Asked {
		return nil
	}
	return []string{fmt.Sprintf(
		"the helper answered %d of %d requests the sessions made, and every one an attempt asks must be answered",
		helper.Answered, helper.Asked)}
}

// invalidReasons collects every reason this run measured something other than the target.
// All reasons are collected so one look at a bundle shows everything that went wrong.
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

	// An unmeasured generator invalidates the run, since its headroom is unknown.
	if !in.Headroom.Measured {
		reasons = append(reasons,
			"the generator was not measured, and a run that cannot say how much room its own generator had cannot say what its numbers are about")
	}
	reasons = append(reasons, generatorReasons("generator", in.Headroom)...)
	reasons = append(reasons, technicianReasons(in.Technicians)...)

	// A target gone before the end leaves the later readings describing nothing that ran.
	if in.Target.StoppedAnswering() {
		reasons = append(reasons,
			"the target stopped answering before the end of the run, so the readings after it describe a system that was not there")
	}

	// A restart splits the readings across two processes, so it invalidates the run.
	if in.Target.Restarted() {
		reasons = append(reasons, fmt.Sprintf(
			"target restarted mid-run: the process answering at the end started at %.0f, not %.0f, so the readings either side describe two systems",
			in.Target.End.StartTimeSeconds, in.Target.Start.StartTimeSeconds))
	}

	for _, breach := range in.SafetyBreaches {
		reasons = append(reasons, "safety limit breached: "+breach)
	}

	reasons = append(reasons, phaseReasons(in)...)
	return reasons
}

// technicianReasons judges the technicians' own allowance by the generator's ceilings; an allowance
// named and never read invalidates, as an unmeasured generator does.
func technicianReasons(headroom *Headroom) []string {
	if headroom == nil {
		return nil
	}
	if !headroom.Measured {
		return []string{"the technician generator was not measured, so how much room its own allowance had is unknown"}
	}
	return generatorReasons("technician generator", *headroom)
}

// generatorReasons collects the ways a run measured the generator it names.
// Only a generator's own allowance gates; a shared box's reading describes the pair.
func generatorReasons(name string, headroom Headroom) []string {
	if !headroom.Measured || headroom.Scope != headroomScopeGenerator {
		return nil
	}

	var reasons []string
	if headroom.CPUHeadroomPercent < minGeneratorCPUHeadroomPercent {
		reasons = append(reasons, fmt.Sprintf(
			"%s had %.1f%% processor headroom (floor %.0f%%), so the run measured the %s",
			name, headroom.CPUHeadroomPercent, minGeneratorCPUHeadroomPercent, name))
	}
	if headroom.MemoryUsedPercent > maxGeneratorMemoryUsedPercent {
		reasons = append(reasons, fmt.Sprintf(
			"%s memory reached %.1f%% (ceiling %.0f%%), so the run measured the %s",
			name, headroom.MemoryUsedPercent, maxGeneratorMemoryUsedPercent, name))
	}
	if headroom.CPURefusedPercent != nil && *headroom.CPURefusedPercent > maxGeneratorCPURefusedPercent {
		reasons = append(reasons, fmt.Sprintf(
			"%s was refused the processor for %.1f%% of the run (ceiling %.0f%%), so its latencies carry its own wait",
			name, *headroom.CPURefusedPercent, maxGeneratorCPURefusedPercent))
	}
	return reasons
}

func phaseReasons(in RunInputs) []string {
	maxErrorRate := defaultMaxErrorRate
	if in.Profile != nil && in.Profile.Safety.MaxErrorRate > 0 {
		maxErrorRate = in.Profile.Safety.MaxErrorRate
	}

	var reasons []string
	for _, phase := range in.Phases {
		// A late generator voids even a rung past the breaking point, whose answer it would set.
		if late := phase.GeneratorLateShare; late != nil && *late > maxGeneratorLateShare {
			reasons = append(reasons, fmt.Sprintf(
				"phase %q began %.1f%% of its dials more than %s past their turn (ceiling %.0f%%), so its arrivals followed the generator",
				phase.Name, *late*100, lateAfter, maxGeneratorLateShare*100))
		}
		if pastTheBreakingPoint(in.BreakingPoint, phase) {
			continue
		}
		// A phase that offered no arrivals has no error rate of its own; its window is the prior tail.
		if phase.OfferedAgentArrivals() && phase.ErrorRate > maxErrorRate {
			reasons = append(reasons, fmt.Sprintf(
				"phase %q error rate %.3f is past the ceiling %.3f, so its numbers describe the error path",
				phase.Name, phase.ErrorRate, maxErrorRate))
		}
		reasons = append(reasons, censusReasons(phase)...)
	}
	return reasons
}

// pastTheBreakingPoint reports whether the phase is a rung at or above where the ladder gave out.
// Lower rungs and the recovery phase are still judged, so a system that stays broken invalidates.
func pastTheBreakingPoint(answer *BreakingPoint, phase PhaseResult) bool {
	if answer == nil || answer.GaveAgents <= 0 {
		return false
	}
	return phase.OfferedConnectedAgents >= answer.GaveAgents
}

// conservationBreaches reports what the target took and did not give back; it fails the run.
// A run that never read its target, or read it across a restart, reports none.
func conservationBreaches(target TargetConservation) []string {
	if !target.Bracketed() || target.Restarted() {
		return nil
	}

	retained := target.RetainedGoroutinesPerOperation()
	if retained <= maxRetainedGoroutinesPerOperation {
		return nil
	}
	return []string{fmt.Sprintf(
		"target retained %.2f goroutines per completed operation (ceiling %.2f) across %d operations: %.0f at the start, %.0f at the end",
		retained, maxRetainedGoroutinesPerOperation, target.Operations,
		target.Start.Goroutines, target.End.Goroutines)}
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

// sortedCopy returns a sorted copy, so a verdict is independent of scenario finish order.
func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
