package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingTarget answers with the processor seconds the test hands it, in order.
type countingTarget struct {
	seconds []float64
	unread  bool
	asked   int
}

func (t *countingTarget) read() (float64, bool) {
	if t.unread {
		t.asked++
		return 0, false
	}
	value := t.seconds[min(t.asked, len(t.seconds)-1)]
	t.asked++
	return value, true
}

func TestPhaseReportsWhatShareOfItsAllowanceTheTargetUsed(t *testing.T) {
	target := &countingTarget{seconds: []float64{10.0, 11.8}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 0.5}

	phase := Phase{Name: "steady", Duration: Duration{Duration: 3 * time.Second}, ConnectedAgents: 10}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	require.NotNil(t, result.TargetBusyPercent, "the target answered at both ends of the phase")
	assert.InDelta(t, 120.0, *result.TargetBusyPercent, 0.001)
}

func TestPhaseWithNoTargetReadingReportsNoBusyness(t *testing.T) {
	busy := TargetBusy{ReadCPUSeconds: (&countingTarget{unread: true}).read, Allowance: 1}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

func TestPhaseWithNoDeclaredAllowanceReportsNoBusyness(t *testing.T) {
	target := &countingTarget{seconds: []float64{0, 2}}
	busy := TargetBusy{ReadCPUSeconds: target.read}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

func TestPhaseWhoseTargetRestartedReportsNoBusyness(t *testing.T) {
	target := &countingTarget{seconds: []float64{900, 3}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 1}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{Busy: busy})
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

func TestARunWithNoTargetToReadTakesNoReadings(t *testing.T) {
	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, PhaseReadings{})
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

func TestEveryPhaseOfAWalkCarriesItsOwnBusyness(t *testing.T) {
	target := &countingTarget{seconds: []float64{0, 1, 1, 2, 2, 3}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 1}

	profile := &Profile{
		SchemaVersion: profileSchemaVersion,
		Name:          "test",
		Family:        FamilyNormal,
		Environment:   EnvRunner,
		Fixture:       FixtureSmall,
		Phases: []Phase{
			{Name: "one", Duration: Duration{Duration: 2 * time.Second}, ConnectedAgents: 10},
			{Name: "two", Duration: Duration{Duration: 2 * time.Second}, ConnectedAgents: 20},
			{Name: "three", Duration: Duration{Duration: 2 * time.Second}, ConnectedAgents: 30},
		},
	}

	results, err := RunPhasesWatched(profile, &recordingFleet{}, &testClock{now: time.Now()}, alwaysRoomToRun, PhaseReadings{Busy: busy})
	require.NoError(t, err)
	require.Len(t, results, 3)

	for _, phase := range results {
		require.NotNil(t, phase.TargetBusyPercent, "phase %q took no reading", phase.Name)
		assert.InDelta(t, 50.0, *phase.TargetBusyPercent, 0.001, "phase %q", phase.Name)
	}
}

func TestParseTargetCPUSecondsReadsTheProcessCounter(t *testing.T) {
	page := `# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 3.89
go_goroutines 42
`
	seconds, ok := ParseTargetCPUSeconds(page)
	require.True(t, ok)
	assert.InDelta(t, 3.89, seconds, 0.0001)
}

func TestParseTargetCPUSecondsReportsAPageWithoutIt(t *testing.T) {
	_, ok := ParseTargetCPUSeconds("go_goroutines 42\n")
	assert.False(t, ok)
}

func TestBundleRefusesAPhaseWithNoBusynessOnARunThatReadTheTarget(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})

	err := bundle.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no target busy-ness")
}

func TestBundleAcceptsAPhaseThatCarriesItsBusyness(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})
	busy := 62.5
	bundle.Phases[0].TargetBusyPercent = &busy

	assert.NoError(t, bundle.Validate())
}

func TestBundleAcceptsNoBusynessWhenTheTargetWasNeverRead(t *testing.T) {
	assert.NoError(t, completeBundle().Validate())
}
