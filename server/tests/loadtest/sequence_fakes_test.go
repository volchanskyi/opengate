package main

import (
	"errors"
	"time"
)

// recordingFleet stands in for a fleet of machines. It records what it was asked
// to do and when, so a sequencer can be stepped through a whole profile without
// a network, a server, or a wait.
type recordingFleet struct {
	connected int
	steps     []fleetStep
	failAfter int
	latency   time.Duration
	outcomes  FleetOutcomes

	// probeCost is wall clock a round trip spends, charged to the phase that
	// took it. A real round trip is a whole connect, handshake and register, so
	// a phase pays for every one of them on top of the time it declares.
	probeCost time.Duration
	clock     *testClock

	// arriveNumerator over arriveDenominator is the share of the machines asked
	// for that turn up. Both zero is a fleet that delivers everything.
	arriveNumerator   int64
	arriveDenominator int64
}

type fleetStep struct {
	// within is the window the fleet was given to reach the level in.
	within time.Duration
	target int
}

func (f *recordingFleet) HoldConnected(within time.Duration, target int) error {
	if f.failAfter > 0 && len(f.steps) >= f.failAfter {
		return errors.New("the fleet stopped answering")
	}
	f.steps = append(f.steps, fleetStep{within: within, target: target})
	// Whatever the level climbed by is what turned up, so a fleet that is asked
	// for more machines reports more arrivals and one that winds down reports
	// none.
	if target > f.connected {
		f.outcomes.Arrived += f.arrivalsFor(int64(target - f.connected))
	}
	f.connected = target
	return nil
}

// arrivalsFor is how many of the machines asked for actually turn up.
func (f *recordingFleet) arrivalsFor(asked int64) int64 {
	if f.arriveDenominator <= 0 {
		return asked
	}
	return asked * f.arriveNumerator / f.arriveDenominator
}

func (f *recordingFleet) Connected() int { return f.connected }

func (f *recordingFleet) ProbeLatency() time.Duration {
	if f.probeCost > 0 && f.clock != nil {
		f.clock.Sleep(f.probeCost)
	}
	if f.latency == 0 {
		return 20 * time.Millisecond
	}
	return f.latency
}

func (f *recordingFleet) Outcomes() FleetOutcomes { return f.outcomes }

// alwaysRoomToRun is a machine with plenty left, so these cases exercise the
// walk rather than the guard beside it.
// unreadTarget is a run with no target to read. Every phase then reports an
// absent busy-ness, which is what these cases are about — they are about the
// walk, not about the target.
var unreadTarget = TargetBusy{}

func alwaysRoomToRun() NodeReading {
	return NodeReading{Measured: true, CPUPercent: 5, MemoryPercent: 10}
}

// testClock advances only when the sequencer asks it to, so a six-minute profile
// is walked in microseconds and the boundaries it records are exact.
type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }
