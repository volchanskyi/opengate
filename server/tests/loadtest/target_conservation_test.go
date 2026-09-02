package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A run is gated on the verdict it wrote about itself, and what the target was
// holding is half of that verdict. Two outcomes come out of the bracket, and
// they are different kinds of thing: a process replaced mid-run means the
// numbers describe two systems, so the run measured nothing; a target that kept
// what it took is a finding about the system, so the run measured it and it was
// found wanting.
//
// The third arm is the one that is easy to leave out: a run that never read its
// target says nothing about it at all.

// classifyWithTarget runs a complete, otherwise unremarkable night against one
// target reading, so each case below differs only in what the target did.
func classifyWithTarget(target TargetConservation) Verdict {
	return Classify(RunInputs{
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{CPUHeadroomPercent: 100},
		Phases:            []PhaseResult{{Name: "connect"}},
		Target:            target,
	})
}

// bracket is one pair of readings across a stated number of operations, with
// the same process at both ends unless a case says otherwise.
func bracket(startGoroutines, endGoroutines float64, operations int) TargetConservation {
	const processStart = 1756761000
	return TargetConservation{
		Start:      TargetHealth{Read: true, Goroutines: startGoroutines, StartTimeSeconds: processStart},
		End:        TargetHealth{Read: true, Goroutines: endGoroutines, StartTimeSeconds: processStart},
		Operations: operations,
	}
}

func TestClassifyIsSilentAboutATargetItNeverRead(t *testing.T) {
	t.Parallel()

	verdict := classifyWithTarget(TargetConservation{})

	assert.Equal(t, ResultValid, verdict.Result)
	assert.Empty(t, verdict.Reasons)
}

func TestClassifyInvalidatesARunWhoseTargetWasReplaced(t *testing.T) {
	t.Parallel()

	replaced := bracket(29, 32, 100)
	replaced.End.StartTimeSeconds = 1756761838

	verdict := classifyWithTarget(replaced)

	require.Equal(t, ResultInvalid, verdict.Result,
		"numbers measured across a process replacement describe two systems, not one")
	require.NotEmpty(t, verdict.Reasons)
	assert.Contains(t, verdict.Reasons[0], "target restarted mid-run")
}

// Not giving the goroutines back is a finding about the system, so it fails the
// run rather than invalidating it — validity.go's own doctrine.
func TestClassifyFailsARunWhoseTargetKeptWhatItTook(t *testing.T) {
	t.Parallel()

	verdict := classifyWithTarget(bracket(29, 2429, 1200))

	require.Equal(t, ResultFailed, verdict.Result)
	require.NotEmpty(t, verdict.Reasons)
	assert.Contains(t, verdict.Reasons[0], "goroutine")
	assert.Contains(t, verdict.Reasons[0], "2.00",
		"the reason must name the per-operation figure, or nobody can act on it")
}

// A target that gave everything back passes, which is the arm that proves the
// gate is not simply always red.
func TestClassifyAcceptsATargetThatGaveItBack(t *testing.T) {
	t.Parallel()

	verdict := classifyWithTarget(bracket(29, 34, 1200))

	assert.Equal(t, ResultValid, verdict.Result)
}
