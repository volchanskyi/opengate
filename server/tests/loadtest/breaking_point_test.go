package main

import (
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

// The breakpoint family exists to say where the system gives out, and until the
// profile writes down what that means the answer is whatever the run happened to
// survive. The 2026-09-09 ladder reached four thousand machines with no errors
// at all and established nothing except that the answer is higher.
func TestTheLadderNamesTheRungThatHeldAndTheRungThatGave(t *testing.T) {
	answer := FindBreakingPoint(gaveOut(), []PhaseResult{
		rung("step-500", 500, 0, 40, busy(20)),
		rung("step-1000", 1000, 0, 55, busy(38)),
		rung("step-2000", 2000, 0.01, 120, busy(70)),
		rung("step-4000", 4000, 0.48, 380, busy(88)),
		rung("step-8000", 8000, 0.77, 900, busy(92)),
		// The ladder ends where it started, so the report can say whether the
		// system came back. It is not a rung and is not searched.
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

// Each term stands on its own, so a ladder that gives out slowly rather than by
// refusing work is still answered.
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

	// A busy-ness that could not be read is not a target at rest, so a rung it
	// is absent from is judged on the terms that were read.
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

// A ladder that reached its top without giving out has an answer too, and it is
// "higher than this" rather than a breaking point.
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

// An answer shaped as an absence — nothing gave out — is satisfied by the
// absence of the whole conversation, so it has to prove it read a rung first.
func TestALadderThatReadNoRungCountsNone(t *testing.T) {
	answer := FindBreakingPoint(gaveOut(), nil)
	require.NotNil(t, answer)
	assert.Zero(t, answer.RungsRead)

	// And a bundle carrying such an answer is not a run anyone can read.
	bundle := completeBundle()
	bundle.BreakingPoint = answer
	require.Error(t, bundle.Validate())
}

// A profile that declares nothing is asking no question, and gets no answer
// rather than an empty one.
func TestAProfileThatDeclaresNoDefinitionGetsNoAnswer(t *testing.T) {
	assert.Nil(t, FindBreakingPoint(nil, []PhaseResult{rung("step-500", 500, 0.9, 9000, busy(99))}))
}

// The family whose whole subject is the breaking point has to say what one is.
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

// The committed ladder carries one, so the family's answer is a reading against
// numbers somebody wrote down rather than whatever the run survived.
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
