package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A run has three outcomes, not two. Valid and failed are both measurements —
// one of a system that held, one of a system that did not. Invalid is the
// third: the run did not measure the system at all, because the generator ran
// out of room, a safety ceiling stopped it, or a scenario produced nothing.
//
// Keeping invalid separate is the whole point. A partial night absorbed as data
// drags the window median down, so the next genuinely slow night compares
// favourably against it and passes.

func validRunInputs() RunInputs {
	start := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	return RunInputs{
		Profile: &Profile{
			Safety: Safety{MaxNodeCPUPercent: 85, MaxNodeMemoryPercent: 90, MaxErrorRate: 0.05},
		},
		ExpectedScenarios: []string{"api-baseline", "concurrent-agents", "relay-throughput", "quic-agents"},
		ProducedScenarios: []string{"api-baseline", "concurrent-agents", "relay-throughput", "quic-agents"},
		Headroom:          Headroom{CPUHeadroomPercent: 60, MemoryUsedPercent: 45},
		Phases: []PhaseResult{{
			Name:                      "steady",
			StartedAt:                 start,
			FinishedAt:                start.Add(time.Minute),
			OfferedArrivalsPerSecond:  5,
			AchievedArrivalsPerSecond: 4.9,
			ErrorRate:                 0.001,
		}},
	}
}

// classify applies one change to a healthy run and returns the verdict, so each
// case below reads as the one thing it is about.
func classify(mutate func(*RunInputs)) Verdict {
	in := validRunInputs()
	if mutate != nil {
		mutate(&in)
	}
	return Classify(in)
}

func TestARunThatMeasuredTheSystemIsValid(t *testing.T) {
	verdict := classify(nil)

	assert.Equal(t, ResultValid, verdict.Result)
	assert.Empty(t, verdict.Reasons)
	assert.True(t, verdict.EntersTrend())
}

// Each of these is a way of not measuring the system. None of them is a slow
// system, and recording any of them as one is what costs the trend two nights.
func TestARunThatDidNotMeasureTheSystemIsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RunInputs)
		reason string
	}{
		{
			name:   "a scenario produced no rows at all",
			mutate: func(in *RunInputs) { in.ProducedScenarios = in.ProducedScenarios[:3] },
			reason: "quic-agents",
		},
		{
			name:   "rows arrived from a scenario nobody asked for",
			mutate: func(in *RunInputs) { in.ProducedScenarios = append(in.ProducedScenarios, "ad-hoc") },
			reason: "ad-hoc",
		},
		{
			name:   "the generator had no processor left",
			mutate: func(in *RunInputs) { in.Headroom.CPUHeadroomPercent = 12 },
			reason: "processor headroom",
		},
		{
			name:   "the generator had nearly no memory left",
			mutate: func(in *RunInputs) { in.Headroom.MemoryUsedPercent = 94 },
			reason: "memory",
		},
		{
			name:   "a safety ceiling stopped the run",
			mutate: func(in *RunInputs) { in.SafetyBreaches = []string{"node processor reached 91%"} },
			reason: "safety",
		},
		{
			name:   "the numbers describe the error path",
			mutate: func(in *RunInputs) { in.Phases[0].ErrorRate = 0.4 },
			reason: "error rate",
		},
		{
			name:   "the load was never offered",
			mutate: func(in *RunInputs) { in.Phases[0].AchievedArrivalsPerSecond = 1.0 },
			reason: "offered",
		},
		{
			name: "a missing profile does not read as an unlimited ceiling",
			mutate: func(in *RunInputs) {
				in.Profile = nil
				in.Phases[0].ErrorRate = 0.9
			},
			reason: "error rate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict := classify(tc.mutate)

			assert.Equal(t, ResultInvalid, verdict.Result)
			assert.False(t, verdict.EntersTrend(), "an unmeasured night must not move the window median")
			assert.Contains(t, strings.Join(verdict.Reasons, "\n"), tc.reason)
		})
	}
}

// The completeness record names both halves, so a reader sees what ran rather
// than inferring it from which rows happened to arrive.
func TestTheVerdictNamesWhatRanAndWhatDidNot(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.ProducedScenarios = []string{"api-baseline", "ad-hoc"}
	})

	assert.ElementsMatch(t, []string{"api-baseline", "ad-hoc"}, verdict.ProducedScenarios)
	assert.ElementsMatch(t,
		[]string{"concurrent-agents", "relay-throughput", "quic-agents"},
		verdict.MissingScenarios)
	assert.ElementsMatch(t, []string{"ad-hoc"}, verdict.UnexpectedScenarios)
}

// A gate breach is a failure, which is a measurement — the system was measured
// and it was too slow. It stays in the trend.
func TestABreachedGateFailsTheRunWithoutInvalidatingIt(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.GateBreaches = []string{"k6/api-baseline/http latency_p95_ms 100 -> 180"}
	})

	assert.Equal(t, ResultFailed, verdict.Result)
	assert.True(t, verdict.EntersTrend(), "a slow night is exactly what the trend is for")
	require.NotEmpty(t, verdict.Reasons)
	assert.Contains(t, verdict.Reasons[0], "latency_p95_ms")
}

// Invalid outranks failed. A run that did not measure the system cannot also
// report that the system was slow.
func TestInvalidOutranksFailed(t *testing.T) {
	verdict := classify(func(in *RunInputs) {
		in.GateBreaches = []string{"some gate"}
		in.ProducedScenarios = nil
	})

	assert.Equal(t, ResultInvalid, verdict.Result)
}
