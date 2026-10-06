package main

import (
	"time"
)

// rampSteps is how many instructions a phase's climb is broken into, so a one-second phase
// and a six-hour one are both climbs.
const rampSteps = 10

// FleetOutcomes is the cumulative tally of what a fleet's machines have seen; a phase is the
// difference between two readings of it.
type FleetOutcomes struct {
	// Arrived is machines that connected, handshook and registered, counted at that moment.
	Arrived int64
	// Failed is machines that never reached registered, counted when their lives end.
	Failed int64
	// Severed is machines whose held connection went away underneath them.
	Severed int64
	// Rejected is refusals the server made on purpose, such as a spent credential or a rate ceiling.
	Rejected int64
	// Departed is registered connections that have since ended, however they ended.
	Departed int64
	// StoodDown is machines the run cancelled before they registered; they are neither an
	// arrival nor a failure to arrive.
	StoodDown int64
}

// Attempted is how many machines produced an outcome either way.
func (o FleetOutcomes) Attempted() int64 { return o.Arrived + o.Failed }

// Measured says whether the error rate beside it is a reading of anything.
// A fleet refused entirely at a declared ceiling attempted nothing, so its zero rate is a guard.
func (o FleetOutcomes) Measured() bool { return o.Attempted() > 0 }

// Since is what happened between an earlier reading and this one.
func (o FleetOutcomes) Since(earlier FleetOutcomes) FleetOutcomes {
	return FleetOutcomes{
		Arrived:   o.Arrived - earlier.Arrived,
		Departed:  o.Departed - earlier.Departed,
		Failed:    o.Failed - earlier.Failed,
		Severed:   o.Severed - earlier.Severed,
		Rejected:  o.Rejected - earlier.Rejected,
		StoodDown: o.StoodDown - earlier.StoodDown,
	}
}

// ErrorRate is the share of attempted machines that did not arrive.
func (o FleetOutcomes) ErrorRate() float64 {
	if o.Attempted() <= 0 {
		return 0
	}
	return float64(o.Failed) / float64(o.Attempted())
}

// Fleet is whatever holds machines connected during a run.
type Fleet interface {
	// HoldConnected asks for exactly this many machines to be connected. The window is how
	// long the fleet has to get there, which a real fleet spends spreading the arrivals.
	HoldConnected(within time.Duration, target int) error
	// Connected is how many are connected now, which can differ from what was asked.
	Connected() int
	// ProbeLatency is a round trip taken now: a machine that connects, handshakes and registers.
	// Zero means the round trip could not be taken.
	ProbeLatency() time.Duration
	// Outcomes is what the fleet's machines have seen so far.
	Outcomes() FleetOutcomes
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
func runOnePhase(phase Phase, from int, fleet Fleet, clock Clock, readings PhaseReadings) (PhaseResult, error) {
	startedAt := clock.Now()
	// Each bracket opens before the climb and closes after the hold, covering exactly this phase.
	closeBusy := readings.Busy.Bracket()
	closeRoom := readings.Generator.Bracket()
	closeDrops := readings.Network.Bracket()
	step := phase.Duration.Duration / rampSteps
	if step <= 0 {
		step = phase.Duration.Duration
	}

	// The outcome tally is cumulative, so the phase's own outcomes are the difference from here.
	began := fleet.Outcomes()

	var samples []time.Duration
	elapsed := time.Duration(0)
	for i := 1; i <= rampSteps; i++ {
		target := levelAt(from, phase.ConnectedAgents, i, rampSteps)
		// The gap until the next step is the window this step has to reach its level in.
		if err := fleet.HoldConnected(step, target); err != nil {
			return PhaseResult{}, err
		}
		// A live round trip at each climb step measures the tail over the phase itself.
		if sample := fleet.ProbeLatency(); sample > 0 {
			samples = append(samples, sample)
		}
		clock.Sleep(step)
		elapsed += step
	}

	// The remainder keeps the phase as long as declared, so the next one starts where this ends.
	if remainder := phase.Duration.Duration - elapsed; remainder > 0 {
		clock.Sleep(remainder)
	}

	// The probes make a phase run longer than declared, so the recorded end is the actual one.
	finishedAt := clock.Now()
	saw := fleet.Outcomes().Since(began)
	seconds := finishedAt.Sub(startedAt).Seconds()

	// The brackets close on the phase's own boundary, before the census holds the run still,
	// since the target's catch-up belongs to neither phase.
	targetBusy, busyAbsent := closeBusy(finishedAt.Sub(startedAt))
	generatorHeadroom, generatorRefused := closeRoom(finishedAt.Sub(startedAt))
	generatorDrops, targetDrops := closeDrops()

	// The run's count, the target's account and the run's count again bracket the target's
	// answer in time. The census holds still until the target catches up and reports how long.
	heldBefore := fleet.Connected()
	departedBefore := fleet.Outcomes().Departed
	census := readings.Census.Take(func() int {
		// The count falls as machines leave, so a shrinking fleet is not awaited for a catch-up.
		return heldBefore - int(fleet.Outcomes().Departed-departedBefore)
	}, clock)
	held := fleet.Connected()
	departedDuring := fleet.Outcomes().Departed - departedBefore

	return PhaseResult{
		Name:       phase.Name,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		// The climb is the offer: machines asked for beyond the previous level, over the phase's clock.
		OfferedAgentArrivalsPerSecond:  ratePerSecond(int64(max(phase.ConnectedAgents-from, 0)), seconds),
		AchievedAgentArrivalsPerSecond: ratePerSecond(saw.Arrived, seconds),
		// The profile's technician figure travels; nothing offers it, so its achieved half is absent.
		OfferedOperatorArrivalsPerSecond: phase.OperatorArrivalsPerSecond,
		OfferedSessions:                  phase.Sessions,
		OfferedConnectedAgents:           phase.ConnectedAgents,
		AchievedConnectedAgents:          held,
		// The target's own count and its lower-bounding readings, absent where it could not be asked.
		TargetConnectedAgents: census.Agents,
		TargetGoroutines:      census.Goroutines,
		TargetResidentBytes:   census.ResidentBytes,
		TargetCensusAbsent:    census.Absent,
		// How long the target took to account for the fleet the run held.
		TargetCensusWaitedMs: millis(census.Waited),
		// The two terms the counts may differ by: held before the census, and departed during it.
		ConnectedAgentsBeforeCensus: heldBefore,
		DeparturesDuringCensus:      departedDuring,
		LatencyP50Ms:                millis(percentile(samples, 50)),
		LatencyP95Ms:                millis(percentile(samples, 95)),
		LatencyP99Ms:                millis(percentile(samples, 99)),
		ErrorRate:                   saw.ErrorRate(),
		// What the target did with its allowance this phase, absent where it could not be read.
		TargetBusyPercent: targetBusy,
		TargetBusyAbsent:  busyAbsent,
		// The generator's room and each end's kernel drops over the same window.
		GeneratorCPUHeadroomPercent: generatorHeadroom,
		GeneratorCPURefusedPercent:  generatorRefused,
		GeneratorUDPReceiveErrors:   generatorDrops,
		TargetUDPReceiveErrors:      targetDrops,
		ExpectedRejections:          saw.Rejected,
		Faults:                      saw.Severed,
	}, nil
}

// ratePerSecond is a count over a window, or zero for a window with no length.
func ratePerSecond(count int64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}
	return float64(count) / seconds
}

// levelAt is how many machines are connected at step i of n, climbing from one level to the next.
// The last step lands exactly on the target.
func levelAt(from, to, i, n int) int {
	if i >= n {
		return to
	}
	return from + (to-from)*i/n
}
