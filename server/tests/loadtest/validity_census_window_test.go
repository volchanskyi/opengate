package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachinesThatLeftWhileTheQuestionWasAskedAreAllowedFor(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(500, 497, 1500)
		phase.ConnectedAgentsBeforeCensus = 500
		phase.DeparturesDuringCensus = 3
		in.Phases = []PhaseResult{phase}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

func TestAShortfallBiggerThanWhatLeftIsNotAMeasurement(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(500, 492, 1500)
		phase.ConnectedAgentsBeforeCensus = 500
		phase.DeparturesDuringCensus = 3
		in.Phases = []PhaseResult{phase}
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "the target was holding 492")
}

func TestTheLowerOfTheRunsTwoCountsIsWhatTheTargetIsHeldTo(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(500, 460, 1400)
		// 460 machines before the question, 500 after it, and the target answered 460.
		phase.ConnectedAgentsBeforeCensus = 460
		in.Phases = []PhaseResult{phase}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}
