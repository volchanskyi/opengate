package main

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPhaseResultSaysWhetherItsProfileMeasuresIt(t *testing.T) {
	quiet := Phase{Name: "ramp", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	measured := Phase{Name: "peak", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1, Measured: true}

	ramp, err := runOnePhase(quiet, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{})
	require.NoError(t, err)
	peak, err := runOnePhase(measured, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{})
	require.NoError(t, err)

	assert.False(t, ramp.Measured)
	assert.True(t, peak.Measured)
}

func registrationAt(accepted float64, under10ms, under1s float64) ServerRegistration {
	return ServerRegistration{Accepted: int64(accepted), Buckets: []bucketBound{
		{le: 0.01, count: under10ms}, {le: 1, count: under1s}, {le: math.Inf(1), count: accepted}}}
}

func TestAPhaseReadsTheServersSignInTimesOverItsOwnSpan(t *testing.T) {
	readings := []ServerRegistration{registrationAt(100, 100, 100), registrationAt(200, 110, 200)}
	asked := 0
	registration := PhaseRegistration{Read: func() (ServerRegistration, bool) {
		reading := readings[min(asked, len(readings)-1)]
		asked++
		return reading, true
	}}
	phase := Phase{Name: "spike", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}

	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Registration: registration})

	require.NoError(t, err)
	require.NotNil(t, result.RegisterP95Ms, "both ends of the phase were read")
	assert.Greater(t, *result.RegisterP95Ms, 10.0, "ninety of the phase's hundred took more than 10 ms")
	assert.LessOrEqual(t, *result.RegisterP95Ms, 1000.0)
	require.NotNil(t, result.RegisterP50Ms)
}

func TestAPhaseNobodySignedInDuringCarriesNoSignInTimes(t *testing.T) {
	registration := PhaseRegistration{Read: func() (ServerRegistration, bool) { return registrationAt(50, 50, 50), true }}
	phase := Phase{Name: "drain", Duration: Duration{Duration: time.Second}}

	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Registration: registration})

	require.NoError(t, err)
	assert.Nil(t, result.RegisterP95Ms)
}
