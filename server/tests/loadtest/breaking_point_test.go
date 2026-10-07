package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func busy(percent float64) *float64 { return &percent }

func rung(name string, agents int, errorRate, p95 float64, target *float64) PhaseResult {
	return PhaseResult{
		Name:                    name,
		OfferedConnectedAgents:  agents,
		AchievedConnectedAgents: agents,
		ErrorRate:               errorRate,
		LatencyP95Ms:            p95,
		TargetBusyPercent:       target,
	}
}

func gaveOut() *GaveOut {
	return &GaveOut{ErrorRateAbove: 0.05, LatencyP95MsAbove: 2000, TargetBusyPercentAbove: 95}
}

func classifyLadder(profile *Profile, phases []PhaseResult) Verdict {
	return Classify(RunInputs{
		Profile:           profile,
		BreakingPoint:     FindBreakingPoint(profile.GaveOut, phases),
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{Measured: true, Scope: headroomScopeGenerator, CPUHeadroomPercent: 90},
		Phases:            phases,
	})
}

func TestTheLadderNamesTheRungThatHeldAndTheRungThatGave(t *testing.T) {
	answer := FindBreakingPoint(gaveOut(), []PhaseResult{
		rung("step-500", 500, 0, 40, busy(20)),
		rung("step-1000", 1000, 0, 55, busy(38)),
		rung("step-2000", 2000, 0.01, 120, busy(70)),
		rung("step-4000", 4000, 0.48, 380, busy(88)),
		rung("step-8000", 8000, 0.77, 900, busy(92)),
		rung("recovery", 500, 0, 45, busy(21)),
	})

	require.NotNil(t, answer)
	assert.Equal(t, "step-2000", answer.HeldAt, "the last rung that held")
	assert.Equal(t, 2000, answer.HeldAgents)
	assert.Equal(t, "step-4000", answer.GaveAt, "the first rung that did not")
	assert.Equal(t, 4000, answer.GaveAgents)
	assert.Contains(t, answer.Reason, "error rate", "and which reading said so")
	assert.Equal(t, 4, answer.RungsRead)
}

func TestWhereItGaveNamesWhatMovedBetweenTheLastTwoRungs(t *testing.T) {
	held := rung("step-8000", 8000, 0, 14, busy(45))
	held.GeneratorCPUHeadroomPercent = float64Of(80)
	held.GeneratorCPURefusedPercent = float64Of(1)
	held.GeneratorUDPReceiveErrors = int64Of(0)
	held.TargetUDPReceiveErrors = int64Of(0)

	gave := rung("step-16000", 16000, 0.3, 6553, busy(60))
	gave.GeneratorCPUHeadroomPercent = float64Of(12)
	gave.GeneratorCPURefusedPercent = float64Of(18)
	gave.GeneratorUDPReceiveErrors = int64Of(48_211)
	gave.TargetUDPReceiveErrors = int64Of(0)

	answer := FindBreakingPoint(gaveOut(), []PhaseResult{held, gave})
	require.NotNil(t, answer)
	assert.Equal(t, []MovedReading{
		{Reading: "target_busy_percent", Held: 45, Gave: 60},
		{Reading: "generator_cpu_headroom_percent", Held: 80, Gave: 12},
		{Reading: "generator_cpu_refused_percent", Held: 1, Gave: 18},
		{Reading: "generator_udp_receive_errors", Held: 0, Gave: 48_211},
	}, answer.Moved, "the target's buffers dropped nothing at either rung, so they are not named")
}

func TestOnlyReadingsBothRungsTookAreCompared(t *testing.T) {
	held := rung("step-8000", 8000, 0, 14, nil)
	gave := rung("step-16000", 16000, 0.3, 6553, busy(60))
	gave.GeneratorUDPReceiveErrors = int64Of(48_211)

	answer := FindBreakingPoint(gaveOut(), []PhaseResult{held, gave})
	require.NotNil(t, answer)
	assert.Empty(t, answer.Moved)

	first := FindBreakingPoint(gaveOut(), []PhaseResult{gave})
	require.NotNil(t, first)
	assert.Empty(t, first.Moved, "the first rung gave, so there is no rung that held to compare it with")
}

func TestTheLogNamesWhatMovedWhereTheLadderGave(t *testing.T) {
	out := captureStdout(t, func() {
		printBreakingPoint(&BreakingPoint{
			HeldAt: "step-8000", HeldAgents: 8000, GaveAt: "step-16000", GaveAgents: 16000,
			Reason: "error rate 0.301 is past 0.050", RungsRead: 6,
			Moved: []MovedReading{{Reading: "generator_udp_receive_errors", Held: 0, Gave: 48211}},
		})
	})

	assert.Contains(t, out, "Gave:        step-16000 (16000 machines)")
	assert.Contains(t, out, "Moved:       generator_udp_receive_errors 0.0 → 48211.0")
}

func float64Of(v float64) *float64 { return &v }
func int64Of(v int64) *int64       { return &v }

func TestEachTermCanBeTheOneThatGives(t *testing.T) {
	t.Run("the wait times", func(t *testing.T) {
		answer := FindBreakingPoint(gaveOut(), []PhaseResult{
			rung("step-500", 500, 0, 40, busy(20)),
			rung("step-1000", 1000, 0, 4200, busy(60)),
		})
		require.NotNil(t, answer)
		assert.Equal(t, "step-1000", answer.GaveAt)
		assert.Contains(t, answer.Reason, "wait")
	})

	t.Run("the target running out of processor", func(t *testing.T) {
		answer := FindBreakingPoint(gaveOut(), []PhaseResult{
			rung("step-500", 500, 0, 40, busy(20)),
			rung("step-1000", 1000, 0, 60, busy(99)),
		})
		require.NotNil(t, answer)
		assert.Equal(t, "step-1000", answer.GaveAt)
		assert.Contains(t, answer.Reason, "processor allowance")
	})

	t.Run("a reading nobody took decides nothing", func(t *testing.T) {
		answer := FindBreakingPoint(gaveOut(), []PhaseResult{
			rung("step-500", 500, 0, 40, nil),
			rung("step-1000", 1000, 0, 60, nil),
		})
		require.NotNil(t, answer)
		assert.Empty(t, answer.GaveAt)
		assert.Equal(t, "step-1000", answer.HeldAt)
	})
}

func TestALadderThatHeldThroughoutSaysSo(t *testing.T) {
	answer := FindBreakingPoint(gaveOut(), []PhaseResult{
		rung("step-500", 500, 0, 40, busy(20)),
		rung("step-1000", 1000, 0, 55, busy(38)),
	})
	require.NotNil(t, answer)
	assert.Equal(t, "step-1000", answer.HeldAt)
	assert.Empty(t, answer.GaveAt)
	assert.Empty(t, answer.Reason)
	assert.Equal(t, 2, answer.RungsRead)
}

func TestALadderThatReadNoRungCountsNone(t *testing.T) {
	answer := FindBreakingPoint(gaveOut(), nil)
	require.NotNil(t, answer)
	assert.Zero(t, answer.RungsRead)

	bundle := completeBundle()
	bundle.BreakingPoint = answer
	require.Error(t, bundle.Validate())
}

func TestAProfileThatDeclaresNoDefinitionGetsNoAnswer(t *testing.T) {
	assert.Nil(t, FindBreakingPoint(nil, []PhaseResult{rung("step-500", 500, 0.9, 9000, busy(99))}))
}

func TestTheBreakpointFamilyMustDeclareWhatGivingOutMeans(t *testing.T) {
	profile := breakpointShapedProfile()
	profile.GaveOut = nil
	err := profile.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gave_out")
}

func TestADefinitionWithNoTermsIsNotADefinition(t *testing.T) {
	profile := breakpointShapedProfile()
	profile.GaveOut = &GaveOut{}
	err := profile.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one")
}

func TestADefinitionWithANegativeTermIsRefused(t *testing.T) {
	profile := breakpointShapedProfile()
	profile.GaveOut = &GaveOut{ErrorRateAbove: -1}
	err := profile.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error_rate_above")
}

func TestTheCommittedBreakpointProfileDeclaresWhatGivingOutMeans(t *testing.T) {
	profiles, err := LoadProfileDir(profileDir())
	require.NoError(t, err)

	var found int
	for _, profile := range profiles {
		if profile.Family != FamilyBreakpoint {
			continue
		}
		found++
		require.NotNil(t, profile.GaveOut, "%s is a breakpoint profile", profile.Name)
	}
	require.Positive(t, found, "the sweep read no breakpoint profile at all")
}

func breakpointShapedProfile() *Profile {
	return &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "ladder",
		Family:        FamilyBreakpoint,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "step-500", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 500},
		},
		Safety:  Safety{MaxNodeMemoryPercent: 80, MaxErrorRate: 0.25},
		GaveOut: gaveOut(),
	}
}

func TestALadderIsNotInvalidatedByTheRungItWasSentToFind(t *testing.T) {
	profile := &Profile{
		Safety:  Safety{MaxErrorRate: 0.25},
		GaveOut: &GaveOut{ErrorRateAbove: 0.05},
	}

	verdict := classifyLadder(profile, []PhaseResult{
		{Name: "step-500", OfferedConnectedAgents: 500, ErrorRate: 0},
		{Name: "step-1000", OfferedConnectedAgents: 1000, ErrorRate: 0.99},
		{Name: "recovery", OfferedConnectedAgents: 500, ErrorRate: 0},
	})

	assert.Equal(t, ResultValid, verdict.Result,
		"the rung that gave out is the finding: %v", verdict.Reasons)
}

func TestARecoveryThatNeverRecoveredStillInvalidatesTheLadder(t *testing.T) {
	profile := &Profile{
		Safety:  Safety{MaxErrorRate: 0.25},
		GaveOut: &GaveOut{ErrorRateAbove: 0.05},
	}

	verdict := classifyLadder(profile, []PhaseResult{
		{Name: "step-500", OfferedConnectedAgents: 500, ErrorRate: 0},
		{Name: "step-1000", OfferedConnectedAgents: 1000, ErrorRate: 0.99},
		{Name: "drain", OfferedConnectedAgents: 0, ErrorRate: 0},
		{
			Name:                           "recovery",
			OfferedConnectedAgents:         500,
			OfferedAgentArrivalsPerSecond:  1.67,
			AchievedAgentArrivalsPerSecond: 1.6,
			ErrorRate:                      0.9,
		},
	})

	assert.Equal(t, ResultInvalid, verdict.Result)
}

func TestAPhaseThatOfferedNoArrivalsIsNotHeldToTheirErrorRate(t *testing.T) {
	profile := &Profile{
		Safety:  Safety{MaxErrorRate: 0.25},
		GaveOut: &GaveOut{ErrorRateAbove: 0.05},
	}

	verdict := classifyLadder(profile, []PhaseResult{
		{Name: "step-8000", OfferedConnectedAgents: 8000, OfferedAgentArrivalsPerSecond: 13.33, AchievedAgentArrivalsPerSecond: 13.33, ErrorRate: 0},
		{Name: "step-16000", OfferedConnectedAgents: 16000, OfferedAgentArrivalsPerSecond: 23.16, AchievedAgentArrivalsPerSecond: 9.89, ErrorRate: 0.5318624092092641},
		{Name: "recovery", OfferedConnectedAgents: 500, OfferedAgentArrivalsPerSecond: 0, AchievedAgentArrivalsPerSecond: 0.023, ErrorRate: 0.5882352941176471},
	})

	assert.Equal(t, ResultValid, verdict.Result,
		"the wind-down's stragglers are the rung before it: %v", verdict.Reasons)
}

func TestAPhaseThatOfferedArrivalsIsStillHeldToItsErrorRate(t *testing.T) {
	profile := &Profile{Safety: Safety{MaxErrorRate: 0.25}}

	verdict := classifyLadder(profile, []PhaseResult{{
		Name:                           "steady",
		OfferedConnectedAgents:         500,
		OfferedAgentArrivalsPerSecond:  5,
		AchievedAgentArrivalsPerSecond: 4.9,
		ErrorRate:                      0.4,
	}})

	assert.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, strings.Join(verdict.Reasons, "\n"), "error rate")
}

func TestTheCommittedLadderRecoversWithMachinesItReachedFor(t *testing.T) {
	profiles, err := LoadProfileDir(profileDir())
	require.NoError(t, err)

	var found int
	for _, profile := range profiles {
		if profile.Family != FamilyBreakpoint {
			continue
		}
		found++
		require.Greater(t, len(profile.Phases), 1, "%s has no recovery phase", profile.Name)
		last := profile.Phases[len(profile.Phases)-1]
		before := profile.Phases[len(profile.Phases)-2]
		assert.Greater(t, last.ConnectedAgents, before.ConnectedAgents,
			"%s's %q phase keeps what the ladder broke instead of reaching for machines of its own",
			profile.Name, last.Name)
	}
	require.Positive(t, found, "the sweep read no breakpoint profile at all")
}

func TestAProfileThatAsksNoCapacityQuestionKeepsItsCeiling(t *testing.T) {
	profile := &Profile{Safety: Safety{MaxErrorRate: 0.25}}

	verdict := classifyLadder(profile, []PhaseResult{{
		Name:                           "steady",
		OfferedConnectedAgents:         500,
		OfferedAgentArrivalsPerSecond:  5,
		AchievedAgentArrivalsPerSecond: 4.9,
		ErrorRate:                      0.99,
	}})

	assert.Equal(t, ResultInvalid, verdict.Result)
}
