package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// How hard the target worked is the reading every other reading in a bundle
// rests on.
//
// A phase reports the wait times it saw and the machines that turned up. From
// those two alone a server that is out of processor and a server that is idle
// but slow are the same picture — and they are different problems with
// different fixes. Every statement of the form "the server was not working
// hard" was an inference until this reading existed.
//
// So the cases below break the reading in each of the ways it can be broken and
// require the run to report an absence rather than a nought. Nought is a target
// that did no work at all, which is the healthiest figure a server could
// report, and a reader that fills an unanswered question in with it hands every
// comparison the one answer that always looks good.

// countingTarget answers with the processor seconds the test hands it, in
// order, and says how many times it was asked.
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

// A target that spent 1.8 processor-seconds over a three-second phase, against
// an allowance of half a processor, used 120% of what it was given — it was
// over its share and the kernel was making it wait for the rest.
func TestPhaseReportsWhatShareOfItsAllowanceTheTargetUsed(t *testing.T) {
	target := &countingTarget{seconds: []float64{10.0, 11.8}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 0.5}

	phase := Phase{Name: "steady", Duration: Duration{Duration: 3 * time.Second}, ConnectedAgents: 10}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	require.NotNil(t, result.TargetBusyPercent, "the target answered at both ends of the phase")
	assert.InDelta(t, 120.0, *result.TargetBusyPercent, 0.001)
}

// A phase against a target nobody could read reports nothing, which is what it
// measured.
func TestPhaseWithNoTargetReadingReportsNoBusyness(t *testing.T) {
	busy := TargetBusy{ReadCPUSeconds: (&countingTarget{unread: true}).read, Allowance: 1}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

// A run that was never told what the target is capped at cannot say what share
// of it was used. The processor seconds are real and the denominator is not, so
// the answer is absent rather than a figure divided by a guess.
func TestPhaseWithNoDeclaredAllowanceReportsNoBusyness(t *testing.T) {
	target := &countingTarget{seconds: []float64{0, 2}}
	busy := TargetBusy{ReadCPUSeconds: target.read}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

// A counter that went backwards is a target that restarted inside the phase.
// The difference between the two readings is then a figure about two
// processes, and subtracting them reports a negative amount of work.
func TestPhaseWhoseTargetRestartedReportsNoBusyness(t *testing.T) {
	target := &countingTarget{seconds: []float64{900, 3}}
	busy := TargetBusy{ReadCPUSeconds: target.read, Allowance: 1}

	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, busy)
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

// A run given no target to read takes no readings at all, rather than reporting
// a target that did no work.
func TestARunWithNoTargetToReadTakesNoReadings(t *testing.T) {
	phase := Phase{Name: "steady", Duration: Duration{Duration: time.Second}, ConnectedAgents: 1}
	result, err := runOnePhase(phase, 0, &recordingFleet{}, &testClock{now: time.Now()}, TargetBusy{})
	require.NoError(t, err)

	assert.Nil(t, result.TargetBusyPercent)
}

// Every phase of a walk carries its own reading, because the whole use of the
// figure is to sit beside that phase's wait times.
func TestEveryPhaseOfAWalkCarriesItsOwnBusyness(t *testing.T) {
	// Half a processor-second per phase-second at every step, against a whole
	// processor: fifty percent, phase after phase.
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

	results, err := RunPhasesWatched(profile, &recordingFleet{}, &testClock{now: time.Now()}, alwaysRoomToRun, busy)
	require.NoError(t, err)
	require.Len(t, results, 3)

	for _, phase := range results {
		require.NotNil(t, phase.TargetBusyPercent, "phase %q took no reading", phase.Name)
		assert.InDelta(t, 50.0, *phase.TargetBusyPercent, 0.001, "phase %q", phase.Name)
	}
}

// The processor time the target has used is on the page the harness already
// reads registration timing and goroutine counts from.
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

// A page that carries no processor counter is not a target that used none.
func TestParseTargetCPUSecondsReportsAPageWithoutIt(t *testing.T) {
	_, ok := ParseTargetCPUSeconds("go_goroutines 42\n")
	assert.False(t, ok)
}

// A run that read the target's own account of itself could have read how hard
// it was working — the two come off the same page — so a phase that carries no
// busy-ness on such a run is a reading somebody dropped, and the bundle refuses
// it rather than entering the trend one measurement short.
func TestBundleRefusesAPhaseWithNoBusynessOnARunThatReadTheTarget(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})

	err := bundle.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no target busy-ness")
}

// The same bundle with the reading present is a run nobody has to infer
// anything about.
func TestBundleAcceptsAPhaseThatCarriesItsBusyness(t *testing.T) {
	bundle := completeBundle()
	bundle.Observations = append(bundle.Observations,
		Observation{At: bundle.Run.StartedAt, Series: "target_goroutines_start", Value: 210})
	busy := 62.5
	bundle.Phases[0].TargetBusyPercent = &busy

	assert.NoError(t, bundle.Validate())
}

// A venue that publishes nothing about the target is not held to a reading it
// could not take. The rule is about a dropped reading, not about every run.
func TestBundleAcceptsNoBusynessWhenTheTargetWasNeverRead(t *testing.T) {
	assert.NoError(t, completeBundle().Validate())
}
