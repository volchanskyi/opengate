package main

import (
	"time"
)

// A profile declares phases and a run has to walk them.
//
// Running a flat count for a fixed time and calling it a profile measures one
// thing: a fleet arriving all at once against a cold server. That is a real
// event — a site whose link came back — but it is not the everyday shape, and a
// system that absorbs a step change and one that absorbs a climb fail in
// different ways. The phases exist so a run can ask for either.
//
// The walk is separated from the dialling. A fleet here is anything that can be
// told to hold a number of machines connected, which lets a whole six-hour
// profile be stepped through in a test without a network and without a wait,
// and lets the boundaries a bundle records be exact rather than approximately
// what the clock happened to say.

// rampSteps is how many instructions a phase's climb is broken into. It is a
// count rather than an interval so a one-second phase and a six-hour one are
// both climbs rather than one climb and one step.
const rampSteps = 10

// Fleet is whatever holds machines connected during a run.
type Fleet interface {
	// HoldConnected asks for exactly this many machines to be connected. The
	// elapsed time is how far into the phase the request is, which a real fleet
	// uses to spread arrivals and a test uses to say what it saw.
	HoldConnected(elapsed time.Duration, target int) error
	// Connected is how many are actually connected now, which is not always what
	// was asked for — and the difference is the finding.
	Connected() int
	// SampleLatency is the round trip a machine is currently seeing.
	SampleLatency() time.Duration
}

// Clock is time, so a run can be walked without waiting for one.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// realClock is time as the run actually experiences it.
type realClock struct{}

// Now reports the current time.
func (realClock) Now() time.Time { return time.Now() }

// Sleep waits.
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// NewRealClock returns the clock a run outside a test uses.
func NewRealClock() Clock { return realClock{} }

// runOnePhase climbs from the level the previous phase left to this phase's own,
// holds there for the rest of the phase, and reports what happened.
func runOnePhase(phase Phase, from int, fleet Fleet, clock Clock) (PhaseResult, error) {
	startedAt := clock.Now()
	step := phase.Duration.Duration / rampSteps
	if step <= 0 {
		step = phase.Duration.Duration
	}

	var samples []time.Duration
	elapsed := time.Duration(0)
	for i := 1; i <= rampSteps; i++ {
		target := levelAt(from, phase.ConnectedAgents, i, rampSteps)
		if err := fleet.HoldConnected(elapsed, target); err != nil {
			return PhaseResult{}, err
		}
		samples = append(samples, fleet.SampleLatency())
		clock.Sleep(step)
		elapsed += step
	}

	// Whatever the division left over, so the phase is exactly as long as it
	// said it would be and the next one starts where this one ended.
	if remainder := phase.Duration.Duration - elapsed; remainder > 0 {
		clock.Sleep(remainder)
	}

	return PhaseResult{
		Name:                      phase.Name,
		StartedAt:                 startedAt,
		FinishedAt:                startedAt.Add(phase.Duration.Duration),
		OfferedArrivalsPerSecond:  phase.OperatorArrivalsPerSecond,
		AchievedArrivalsPerSecond: phase.OperatorArrivalsPerSecond,
		OfferedConnectedAgents:    phase.ConnectedAgents,
		AchievedConnectedAgents:   fleet.Connected(),
		LatencyP50Ms:              millis(percentile(samples, 50)),
		LatencyP95Ms:              millis(percentile(samples, 95)),
		LatencyP99Ms:              millis(percentile(samples, 99)),
	}, nil
}

// levelAt is how many machines are connected at step i of n, climbing from one
// level to the next. The last step lands exactly on the target rather than near
// it, because a phase that ends one machine short of its own declaration is a
// phase nobody can compare against another run.
func levelAt(from, to, i, n int) int {
	if i >= n {
		return to
	}
	return from + (to-from)*i/n
}
