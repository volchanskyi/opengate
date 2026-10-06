package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPhaseSaysTheTargetWouldNotAnswerWhenItWouldNot(t *testing.T) {
	busy := TargetBusy{ReadCPUSeconds: (&countingTarget{unread: true}).read, Allowance: 1}

	phase := Phase{Name: "step-16000", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
	assert.Equal(t, busyAbsentTargetSilent, result.TargetBusyAbsent,
		"a target that would not answer is the finding, not a dropped reading")
}

func TestAPhaseSaysWhenNobodyDeclaredWhatTheTargetWasCappedAt(t *testing.T) {
	target := &countingTarget{seconds: []float64{0, 2}}
	busy := TargetBusy{ReadCPUSeconds: target.read}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Equal(t, busyAbsentNoAllowance, result.TargetBusyAbsent)
}

func TestAPhaseSaysWhenTheTargetRestartedUnderIt(t *testing.T) {
	target := &countingTarget{seconds: []float64{900, 3}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 1}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Equal(t, busyAbsentTargetRestarted, result.TargetBusyAbsent)
}

func TestARunWithNoTargetAccountsForNothing(t *testing.T) {
	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{})
	require.NoError(t, err)

	assert.Empty(t, result.TargetBusyAbsent)
}

func TestAPhaseThatWasReadAccountsForNoAbsence(t *testing.T) {
	target := &countingTarget{seconds: []float64{10.0, 11.8}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 0.5}

	phase := Phase{Name: "steady", Duration: Duration{Duration: 3 * time.Second}, ConnectedAgents: 10}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Empty(t, result.TargetBusyAbsent)
}

func TestBundleAcceptsAPhaseWhoseAbsenceTheRunAccountsFor(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})
	bundle.Phases[0].TargetBusyAbsent = busyAbsentTargetSilent

	assert.NoError(t, bundle.Validate())
}

func TestBundleStillRefusesAnAbsenceNobodyAccountedFor(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})
	bundle.Phases[0].TargetBusyAbsent = ""

	err := bundle.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no target busy-ness")
}

func TestBundleRefusesAPhaseThatBothReadsAndAccountsForAnAbsence(t *testing.T) {
	bundle := completeBundle()
	busy := 62.5
	bundle.Phases[0].TargetBusyPercent = &busy
	bundle.Phases[0].TargetBusyAbsent = busyAbsentTargetSilent

	err := bundle.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accounts for an absence")
}
