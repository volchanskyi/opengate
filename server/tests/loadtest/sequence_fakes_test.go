package main

import (
	"errors"
	"time"
)

type recordingFleet struct {
	connected int
	steps     []fleetStep
	failAfter int
	latency   time.Duration
	outcomes  FleetOutcomes

	// probeCost is wall clock a round trip spends, charged to the phase that took it.
	probeCost time.Duration
	clock     *testClock

	// arriveNumerator over arriveDenominator is the share of machines asked for that turn up.
	arriveNumerator   int64
	arriveDenominator int64

	// lateNumerator over lateDenominator is the share of dials the generator began past their turn.
	lateNumerator   int64
	lateDenominator int64

	// asked is every machine the fleet was asked for, the mark a wave is read between.
	asked int
	// wave is what the fleet reports of any stretch; its machines stay pending until settlesAt.
	wave      Wave
	settlesAt time.Time
	waveAsked [][2]int
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
	if target > f.connected {
		asked := int64(target - f.connected)
		f.asked += target - f.connected
		f.outcomes.Arrived += f.arrivalsFor(asked)
		f.outcomes.Begun += asked
		f.outcomes.Late += asked * f.lateNumerator / max(f.lateDenominator, 1)
	}
	f.connected = target
	return nil
}

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

func (f *recordingFleet) Asked() int { return f.asked }

func (f *recordingFleet) Wave(from, to int) Wave {
	f.waveAsked = append(f.waveAsked, [2]int{from, to})
	wave := f.wave
	if f.clock == nil || !f.clock.now.Before(f.settlesAt) {
		wave.Pending = 0
	}
	return wave
}

var unreadTarget = PhaseReadings{}

func alwaysRoomToRun() NodeReading {
	return NodeReading{Measured: true, CPUPercent: 5, MemoryPercent: 10}
}

// testClock advances only when asked, and tickOnNow adds time that passes between readings.
type testClock struct {
	now       time.Time
	tickOnNow time.Duration
}

func (c *testClock) Now() time.Time {
	c.now = c.now.Add(c.tickOnNow)
	return c.now
}

func (c *testClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }
