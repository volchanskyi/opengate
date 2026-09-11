package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Why a reading is absent is itself a reading.
//
// A phase that carries no busy-ness voids the whole bundle, on the reasoning
// that a run which read the target once could have read it again — so a phase
// that did not is a reading somebody dropped. There is a third case that
// reasoning does not hold for, and it is the one a capacity ladder exists to
// reach: a target loaded until it stops answering. The nightly ladder found it
// at sixteen thousand machines, could not read the exposition within fifteen
// seconds, and threw away the evidence for the rung it had just climbed.
//
// So the phase says which of the two it was, and the bundle keeps the absence
// whose cause the run can account for.

func TestAPhaseSaysTheTargetWouldNotAnswerWhenItWouldNot(t *testing.T) {
	busy := TargetBusy{ReadCPUSeconds: (&countingTarget{unread: true}).read, Allowance: 1}

	phase := Phase{Name: "step-16000", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
	assert.Equal(t, busyAbsentTargetSilent, result.TargetBusyAbsent,
		"a target that would not answer is the finding, not a dropped reading")
}

func TestAPhaseSaysWhenNobodyDeclaredWhatTheTargetWasCappedAt(t *testing.T) {
	target := &countingTarget{seconds: []float64{0, 2}}
	busy := TargetBusy{ReadCPUSeconds: target.read}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Equal(t, busyAbsentNoAllowance, result.TargetBusyAbsent)
}

func TestAPhaseSaysWhenTheTargetRestartedUnderIt(t *testing.T) {
	target := &countingTarget{seconds: []float64{900, 3}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 1}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Equal(t, busyAbsentTargetRestarted, result.TargetBusyAbsent)
}

// A run pointed at no target asked nothing, so there is no absence to account
// for and no question anybody left unanswered.
func TestARunWithNoTargetAccountsForNothing(t *testing.T) {
	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, TargetBusy{})
	require.NoError(t, err)

	assert.Empty(t, result.TargetBusyAbsent)
}

// A phase that answered fully says nothing about an absence, because there is
// not one.
func TestAPhaseThatWasReadAccountsForNoAbsence(t *testing.T) {
	target := &countingTarget{seconds: []float64{10.0, 11.8}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 0.5}

	phase := Phase{Name: "steady", Duration: Duration{Duration: 3 * time.Second}, ConnectedAgents: 10}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Empty(t, result.TargetBusyAbsent)
}

// A phase whose absence the run can account for keeps its place in the bundle.
// The breakpoint family's whole subject is the load at which the target stops
// coping, and a target that has stopped answering its own exposition is that
// answer rather than a dropped reading.
func TestBundleAcceptsAPhaseWhoseAbsenceTheRunAccountsFor(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})
	bundle.Phases[0].TargetBusyAbsent = busyAbsentTargetSilent

	assert.NoError(t, bundle.Validate())
}

// An absence with no cause beside it is still the dropped reading the rule was
// written for.
func TestBundleStillRefusesAnAbsenceNobodyAccountedFor(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})
	bundle.Phases[0].TargetBusyAbsent = ""

	err := bundle.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no target busy-ness")
}

// A phase cannot both carry a reading and account for its absence: one of the
// two is untrue, and a reader has no way to tell which.
func TestBundleRefusesAPhaseThatBothReadsAndAccountsForAnAbsence(t *testing.T) {
	bundle := completeBundle()
	busy := 62.5
	bundle.Phases[0].TargetBusyPercent = &busy
	bundle.Phases[0].TargetBusyAbsent = busyAbsentTargetSilent

	err := bundle.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accounts for an absence")
}
