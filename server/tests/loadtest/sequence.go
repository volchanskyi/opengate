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

// FleetOutcomes is the running tally of what a fleet's machines have seen. It
// is cumulative rather than per-phase, and a phase is the difference between
// two readings of it.
type FleetOutcomes struct {
	// Arrived is machines that connected, handshook and registered, counted at
	// the moment they did. A fleet holds its machines long past the phase they
	// arrived in, so counting them when their lives end counts them in a phase
	// they had nothing to do with — or, for a fleet held to the end of the
	// walk, in none at all.
	Arrived int64
	// Failed is machines that never reached registered, counted when their
	// lives end, which is the first moment anything knows. A machine that
	// arrived and was later cut off is a fault rather than one of these.
	Failed int64
	// Severed is machines whose held connection went away underneath them,
	// which is the only detector of a fleet cut off mid-run.
	Severed int64
	// Rejected is refusals the server made on purpose — a spent credential, a
	// rate past a declared ceiling. Counting those as faults makes a correctly
	// enforced limit look like a defect and buries the real ones.
	Rejected int64
	// Departed is connections that had registered and have since ended, however
	// they ended. It is the other end of a machine being attached rather than
	// of a machine existing — one that flaps counts a departure each time — and
	// it is what says how much of a difference between two counts of one
	// population is the population itself changing between the two readings.
	Departed int64
	// StoodDown is machines the run cancelled before they ever registered,
	// which is what a wind-down does to every start still reaching for the
	// server when the level comes down.
	//
	// They are the run's own doing and say nothing about the system, so they
	// are neither an arrival nor a failure to arrive — the rule the ramp
	// already states about a machine let go before its turn, kept here where
	// the tally is. Counted rather than dropped, because a phase that stood
	// most of its fleet down measured less than it looks like it did.
	StoodDown int64
}

// Attempted is how many machines produced an outcome either way.
func (o FleetOutcomes) Attempted() int64 { return o.Arrived + o.Failed }

// Measured says whether the error rate beside it is a reading of anything.
//
// A refusal counts in neither term above, so a fleet refused entirely at a
// declared ceiling has attempted nothing and its error rate comes back as zero
// through the guard against dividing by nothing rather than through anything
// having been measured. That zero and a clean run's zero are the same number and
// different facts, and a reader cannot tell them apart without this.
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
	// HoldConnected asks for exactly this many machines to be connected. The
	// window is how long the fleet has to get there, which a real fleet uses to
	// spread the arrivals it is adding and a test uses to say what it saw.
	//
	// It is a window rather than how far into the phase the request is, because
	// how far in says nothing a fleet can act on: it was passed for the life of
	// this interface, read by nothing, and every step of every climb went out as
	// a burst under an offer the profile had written down as a rate.
	HoldConnected(within time.Duration, target int) error
	// Connected is how many are actually connected now, which is not always what
	// was asked for — and the difference is the finding.
	Connected() int
	// ProbeLatency is a round trip taken now: a machine that connects,
	// handshakes and registers while the phase is at its level.
	//
	// It is a fresh arrival rather than a reading off a machine already
	// connected because the control stream has no reply to a heartbeat, so a
	// connect-handshake-register is the only live round trip the machine side
	// has. Zero is a round trip that could not be taken, which is absent rather
	// than instant.
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
func runOnePhase(phase Phase, from int, fleet Fleet, clock Clock, target TargetReading) (PhaseResult, error) {
	startedAt := clock.Now()
	// Opened before the climb and closed after the hold, so what the figure
	// divides is the work the target did in this phase by the time this phase
	// took.
	closeBusy := target.Busy.Bracket()
	step := phase.Duration.Duration / rampSteps
	if step <= 0 {
		step = phase.Duration.Duration
	}

	// The tally the phase's own outcomes are the difference from. It is
	// cumulative across the run, so a phase can only be told apart from the run
	// around it by bracketing it.
	began := fleet.Outcomes()

	var samples []time.Duration
	elapsed := time.Duration(0)
	for i := 1; i <= rampSteps; i++ {
		target := levelAt(from, phase.ConnectedAgents, i, rampSteps)
		// The gap until the next step is what this one has to reach its level
		// in, so the machines it adds arrive at the rate the phase declares
		// rather than all at once.
		if err := fleet.HoldConnected(step, target); err != nil {
			return PhaseResult{}, err
		}
		// A live round trip at each step of the climb, so the phase's tail is
		// measured over the phase rather than read off whichever machine
		// happened to finish last — which in a profiled run is none of them.
		if sample := fleet.ProbeLatency(); sample > 0 {
			samples = append(samples, sample)
		}
		clock.Sleep(step)
		elapsed += step
	}

	// Whatever the division left over, so the phase is exactly as long as it
	// said it would be and the next one starts where this one ended.
	if remainder := phase.Duration.Duration - elapsed; remainder > 0 {
		clock.Sleep(remainder)
	}

	// When the phase actually ended, which is not when it said it would. A
	// round trip taken at each step of a climb costs the phase whatever it
	// costs, and a declared boundary hides that: a sweep leg whose profile
	// asked for three and a half minutes ran for fifteen and forty-two, with
	// every phase in its bundle claiming the declaration.
	finishedAt := clock.Now()
	saw := fleet.Outcomes().Since(began)
	seconds := finishedAt.Sub(startedAt).Seconds()

	// Closed on the phase's own boundary, before the census below is allowed to
	// hold the run still. The work the target does while it catches up with its
	// own fleet belongs to neither phase, and charging it to this one against
	// this one's clock reports a busy-ness nobody measured.
	targetBusy, busyAbsent := closeBusy(finishedAt.Sub(startedAt))

	// The run's own count of its arrived machines, the target's account of the
	// same population, and the run's count again — in that order, so the
	// target's answer is bracketed in time by the run's rather than compared
	// against one taken at a different moment. A phase reporting the run's
	// count alone reports whether its own wind-down code ran.
	//
	// The census is given the count to account for, because the run's count
	// leads the target's by the target's own admission work and a target that
	// is merely behind catches up the moment the run stops offering arrivals.
	// It holds still until it does, and says how long that took.
	heldBefore := fleet.Connected()
	departedBefore := fleet.Outcomes().Departed
	census := target.Census.Take(func() int {
		// The same floor the validity rule applies, asked live: what the run was
		// holding before it asked, less what the fleet has recorded leaving
		// since. It falls as machines leave, so a fleet shrinking under the
		// question is not waited on for a catch-up that can never happen.
		return heldBefore - int(fleet.Outcomes().Departed-departedBefore)
	}, clock)
	held := fleet.Connected()
	departedDuring := fleet.Outcomes().Departed - departedBefore

	return PhaseResult{
		Name:      phase.Name,
		StartedAt: startedAt,
		// The reading, so the phase after it starts where this one ended.
		FinishedAt: finishedAt,
		// What the phase's own climb asked for, against what turned up. The
		// climb is the offer: a phase going from four hundred machines to five
		// hundred over a minute is offering a hundred arrivals in that minute,
		// and a phase that winds down offers none.
		//
		// Both halves are counts over the phase's own clock, so what they
		// compare is machines asked for against machines that turned up,
		// whatever the wall clock did in between.
		OfferedAgentArrivalsPerSecond:  ratePerSecond(int64(max(phase.ConnectedAgents-from, 0)), seconds),
		AchievedAgentArrivalsPerSecond: ratePerSecond(saw.Arrived, seconds),
		// The profile's technician figure travels; nothing here offers it, so
		// its achieved half stays absent.
		OfferedOperatorArrivalsPerSecond: phase.OperatorArrivalsPerSecond,
		OfferedSessions:                  phase.Sessions,
		OfferedConnectedAgents:           phase.ConnectedAgents,
		AchievedConnectedAgents:          held,
		// The other end's count of it, and the reading that bounds that count
		// below. Absent where the target could not be asked.
		TargetConnectedAgents: census.Agents,
		TargetGoroutines:      census.Goroutines,
		TargetCensusAbsent:    census.Absent,
		// How long the target took to account for the fleet the run was
		// holding. It is a reading of how far behind its own arrivals the
		// target was, which nothing else produces.
		TargetCensusWaitedMs: millis(census.Waited),
		// The two terms the counts are allowed to differ by: what the run was
		// holding before it asked, and what left before the answer came back.
		ConnectedAgentsBeforeCensus: heldBefore,
		DeparturesDuringCensus:      departedDuring,
		LatencyP50Ms:                millis(percentile(samples, 50)),
		LatencyP95Ms:                millis(percentile(samples, 95)),
		LatencyP99Ms:                millis(percentile(samples, 99)),
		ErrorRate:                   saw.ErrorRate(),
		// What the target did with the allowance it was given while this phase
		// ran, beside the wait times the phase produced. Absent where it could
		// not be read.
		TargetBusyPercent:  targetBusy,
		TargetBusyAbsent:   busyAbsent,
		ExpectedRejections: saw.Rejected,
		Faults:             saw.Severed,
	}, nil
}

// ratePerSecond is a count over a window, or zero for a window with no length —
// a rate over no time is not a fast run.
func ratePerSecond(count int64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}
	return float64(count) / seconds
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
