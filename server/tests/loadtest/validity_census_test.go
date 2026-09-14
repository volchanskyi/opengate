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

// heldPhase is a phase holding a level, with the target agreeing about it.
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

// The other direction is a gauge, not a missing fleet. The server refreshes its
// count on an interval, and every profile ends by standing its fleet down inside
// a phase shorter than that — so a target reporting more than the phase claims
// is one that has not seen the wind-down yet.
func TestAPhaseWhoseTargetHoldsMoreThanItClaimsIsStillAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		drain := heldPhase(0, 120, 400)
		drain.Name = "drain"
		in.Phases = []PhaseResult{drain}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// A few machines either way is the refresh interval, not a fleet that was never
// there.
func TestAPhaseAFewMachinesShortOfItsClaimIsStillAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 495, 1500)}
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

// A reading that was not taken is not a reading of nought. A phase with no
// census of the target is one nobody could ask, and an unasked question is
// neither a pass nor a failure.
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

// The two counts are comparable only where the target has had time to see the
// level. The server refreshes its own count on an interval, and a phase climbs
// across its whole length in equal steps — so a phase whose last step is
// shorter than that interval closes while the count beside it still describes a
// level the climb has already left. The spike family's own spike is thirty
// seconds, which is three seconds a step: its count would read the level from
// five steps back and the phase would be refused for climbing.
func TestAPhaseShorterThanTheTargetsRefreshIsNotJudgedOnTheTargetsCount(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(2000, 1700, 5200)
		phase.Name = "spike"
		phase.FinishedAt = phase.StartedAt.Add(30 * time.Second)
		in.Phases = []PhaseResult{phase}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// And the same phase held long enough for the count to catch up is judged on
// it. The difference is the phase's own length, not what it was holding.
func TestAPhaseHeldLongEnoughIsJudgedOnTheTargetsCount(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(2000, 1700, 5200)
		phase.Name = "steady"
		phase.FinishedAt = phase.StartedAt.Add(5 * time.Minute)
		in.Phases = []PhaseResult{phase}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "the target was holding 1700")
}
