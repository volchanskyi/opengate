package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldPhase is a phase holding a level that the target agrees with, with no departures.
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

func TestAPhaseWhoseTargetHoldsTheFleetIsAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 500, 1530)}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

func TestAPhaseWhoseTargetDoesNotHoldTheFleetIsNotAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 7, 50)}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "the target was holding 7",
		"the reason names both counts")
}

func TestAPhaseWhoseTargetHoldsMoreThanItClaimsIsStillAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		drain := heldPhase(0, 120, 400)
		drain.Name = "drain"
		in.Phases = []PhaseResult{drain}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

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

func TestATargetWithNoGoroutinesForTheFleetIsNotAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.Phases = []PhaseResult{heldPhase(500, 500, 120)}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "goroutines",
		"the reason names the floor")
}

func TestARungPastTheBreakingPointIsExemptFromBothCounts(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		rung := heldPhase(16000, 439, 1400)
		rung.Name = "step-16000"
		in.Phases = []PhaseResult{rung}
		in.BreakingPoint = &BreakingPoint{GaveAt: "step-16000", GaveAgents: 16000, RungsRead: 6}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

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
