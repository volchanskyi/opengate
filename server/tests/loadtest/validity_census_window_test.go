package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the two counts of one fleet may differ by.
//
// They describe one instant — the run counts its own arrived machines, asks the
// target, and counts again — so the only honest allowance is the population
// itself changing in between, which the fleet counts. There is no share of the
// fleet here and deliberately none: the shortfall a stale count produces is the
// arrival rate times its staleness, which has nothing to do with fleet size, and
// a share wide enough to swallow it is wide enough to swallow the finding.

// What the fleet recorded leaving while the question was in flight is what the
// two counts are allowed to differ by, because the population really did shrink
// between them. The soak replaces every machine it loses, so this is its
// ordinary condition rather than an edge.
func TestMachinesThatLeftWhileTheQuestionWasAskedAreAllowedFor(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(500, 497, 1500)
		phase.ConnectedAgentsBeforeCensus = 500
		phase.DeparturesDuringCensus = 3
		in.Phases = []PhaseResult{phase}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}

// And a shortfall bigger than what left is not explained by the question taking
// time. Three machines left and eight are missing: five of them are a fleet the
// target was not holding.
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

// The run's two counts bracket the target's answer, and the lower of them is
// what the target is held to — so a climb that added machines while the
// question was in flight is not a shortfall.
func TestTheLowerOfTheRunsTwoCountsIsWhatTheTargetIsHeldTo(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		phase := heldPhase(500, 460, 1400)
		// The climb was still landing machines: 460 before the question, 500
		// after it, and the target answered 460 in between.
		phase.ConnectedAgentsBeforeCensus = 460
		in.Phases = []PhaseResult{phase}
	})

	assert.Equal(t, ResultValid, verdict.Result, "reasons: %v", verdict.Reasons)
}
