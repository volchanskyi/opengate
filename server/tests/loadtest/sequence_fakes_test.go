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
}

type fleetStep struct {
	at     time.Duration
	target int
}

func (f *recordingFleet) HoldConnected(elapsed time.Duration, target int) error {
	if f.failAfter > 0 && len(f.steps) >= f.failAfter {
		return errors.New("the fleet stopped answering")
	}
	f.steps = append(f.steps, fleetStep{at: elapsed, target: target})
	f.connected = target
	return nil
}

func (f *recordingFleet) Connected() int { return f.connected }

func (f *recordingFleet) SampleLatency() time.Duration {
	if f.latency == 0 {
		return 20 * time.Millisecond
	}
	return f.latency
}

// alwaysRoomToRun is a machine with plenty left, so these cases exercise the
// walk rather than the guard beside it.
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
