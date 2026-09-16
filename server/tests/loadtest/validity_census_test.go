package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A phase reports what the harness believes it holds. That figure is
// bookkeeping the wind-down maintains — it answers whether the wind-down code
// ran — so a phase whose target was holding nothing published a level anyway,
// and no gate anywhere disagreed. These are the rules that disagree.
//
// The two counts are of one population and are taken at one instant: the run
// counts its own arrived machines, asks the target, and counts again, so the
// target's answer is bracketed by the run's. What is allowed between them is
// what the fleet itself recorded leaving, and nothing else.

// heldPhase is a phase holding a level, with the target agreeing about it and
// nothing leaving while the question was asked.
func heldPhase(claimed int, targetHolds int, goroutines float64) PhaseResult {
	start := time.Date(2026, 9, 13, 2, 0, 0, 0, time.UTC)
	held := targetHolds
	return PhaseResult{
		Name:                           "steady",
		StartedAt:                      start,
		FinishedAt:                     start.Add(5 * time.Minute),
		OfferedAgentArrivalsPerSecond:  5,
		AchievedAgentArrivalsPerSecond: 5,
		OfferedConnectedAgents:         claimed,
		AchievedConnectedAgents:        claimed,
		ConnectedAgentsBeforeCensus:    claimed,
		ErrorRate:                      0,
		TargetConnectedAgents:          &held,
		TargetGoroutines:               &goroutines,
	}
}

// The pair as it was measured: the two counts matched exactly at five hundred
// machines, and the goroutines were three to a machine.
func TestAPhaseWhoseTargetHoldsTheFleetIsAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 500, 1530)}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// The defect. A recovery phase published a figure describing a target still
// carrying the full fleet, on a server that was holding seven machines.
func TestAPhaseWhoseTargetDoesNotHoldTheFleetIsNotAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 7, 50)}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "the target was holding 7",
		"the reason names both counts")
}

// The other direction is a machine that arrived while the question was in
// flight, which is the run's own count catching up rather than a fleet that was
// never there. Only a shortfall is a finding.
func TestAPhaseWhoseTargetHoldsMoreThanItClaimsIsStillAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		drain := heldPhase(0, 120, 400)
		drain.Name = "drain"
		in.Phases = []PhaseResult{drain}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// A count the target was never asked for is not a count of nought. A phase that
// could not read it says why, and is judged on what it does carry.
func TestAPhaseWithNoCensusIsJudgedOnWhatItDoesCarry(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(500, 0, 0)
		phase.TargetConnectedAgents = nil
		phase.TargetGoroutines = nil
		phase.TargetCensusAbsent = censusAbsentTargetSilent
		in.Phases = []PhaseResult{phase}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// The count bounded below. The listener runs a goroutine per accepted
// connection, so a target claiming a fleet it has no goroutines for is
// reporting a number rather than a population.
func TestATargetWithNoGoroutinesForTheFleetIsNotAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 500, 120)}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "goroutines",
		"the reason names the floor")
}

// A capacity ladder is sent to find the rung where the system gives, and the
// machines lost reaching it are the reading rather than a fault — the same
// exemption the error ceiling already carries.
func TestARungPastTheBreakingPointIsExemptFromBothCounts(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		rung := heldPhase(16000, 439, 1400)
		rung.Name = "step-16000"
		in.Phases = []PhaseResult{rung}
		in.BreakingPoint = &BreakingPoint{GaveAt: "step-16000", GaveAgents: 16000, RungsRead: 6}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// A short phase is judged like any other. The target works its count out when
// the page is read, so there is no interval for a phase to be shorter than —
// the spike family's thirty-second spike says what it was holding just as the
// five-minute steady does.
func TestAShortPhaseIsJudgedOnTheTargetsCountLikeAnyOther(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(2000, 1700, 5200)
		phase.Name = "spike"
		phase.FinishedAt = phase.StartedAt.Add(30 * time.Second)
		in.Phases = []PhaseResult{phase}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "the target was holding 1700")
}
