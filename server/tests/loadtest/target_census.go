package main

import "time"

// What the target says it is holding, taken where a phase closes.
//
// A phase already reports a level, and that level is `len(running)` over the
// machines the fleet has not wound down — bookkeeping the wind-down itself
// maintains. It answers whether the wind-down code ran, and the wind-down code
// ran. Between the two readings that bracket a run sat every phase of every
// profiled run, unwatched: two families published a recovery figure describing a
// target still carrying the full fleet on every night they ran, and nothing
// anywhere disagreed.
//
// It matters most for a phase that reaches for no machine of its own. Such a
// phase's error rate is the tail of the phase before it and is held to no
// ceiling, and its attainment is one by construction — so the level it holds is
// the whole of what it could be judged on.
//
// The two numbers here are the other end's account of the same population.
// `opengate_agents_connected` is the server's own count, kept independently of
// the harness's; `go_goroutines` is a reading rather than bookkeeping and
// carries at least one goroutine per accepted connection, so it bounds the pair
// below. Both come off the page the busy-ness reading beside them already uses.

// Why a census is absent, in the phase's own words.
//
// An absence a reader cannot account for is a reading somebody dropped. The
// first of these is the one a capacity ladder climbs to reach — a target loaded
// until it stops answering — and it is a finding rather than a lapse.
const (
	censusAbsentTargetSilent = "the target did not answer when the phase closed"
	censusAbsentNoFleetCount = "the target published no count of the fleet it holds"
)

// The run's count of the fleet leads the target's, and the lead is the target's
// own admission work.
//
// A machine has dialled, handshaken and asked to register before the target has
// put it in the map it publishes a count of, and between those two moments the
// target reads the machine's customer and its name out of the database. So the
// two counts differ by the arrival rate times those reads: exactly nothing on a
// phase that offers no arrivals, and tens of machines on a climbing one against
// a target with no processor left. On the
// night of 2026-09-16 a phase holding 7,946 machines was refused for a target
// holding 7,883, and one holding 1,947 for a target holding 1,891 — with
// nothing failing, nothing severed and nothing leaving. Divide each shortfall
// by its arrival rate and the answer is 1.8 and 7.0 seconds, against targets
// whose own registration figure averaged 0.7 and 9.6 seconds on the same runs.
// The goroutine count taken in the same read agreed with the run rather than
// with the target: those machines were connections the target was holding and
// had not yet admitted.
//
// So the question is asked of a settled population rather than answered with a
// wider allowance. The run holds still while the target admits what it has
// already accepted, and the phase says how long that took — which is a reading
// of how far behind its own fleet the target was, and a number nothing else
// produces.

const (
	// censusSettleInterval is how often the run asks again while the target is
	// still behind. Frequent enough to see a target catching up inside the
	// widest arrival it has a bucket for, so the wait a phase records is a
	// measurement rather than a rounding of one.
	censusSettleInterval = 500 * time.Millisecond

	// censusSettleLimit is how long the run is willing to hold still for.
	//
	// It bounds patience, never the verdict: no shortfall is forgiven by it and
	// none is created by it. What it has to clear is the target's own arrival
	// work, and the widest arrival the target can describe is the last bucket
	// of the histogram it publishes registration in — ten seconds, past which a
	// registration reads as a floor rather than a measurement. Thirty clears it
	// three times over, and
	// TestTheWaitClearsTheWidestArrivalTheTargetCanReport holds the two
	// together so neither can move alone.
	censusSettleLimit = 30 * time.Second
)

// TargetCensus is how a phase asks the target what it is holding.
//
// A run pointed at no target has nothing to ask, which is different from asking
// and getting no answer: there is no absence to account for where there was no
// question.
type TargetCensus struct {
	// Read is one reading of the target's own account of itself, and whether
	// the page answered at all.
	Read func() (TargetHealth, bool)
}

// NewTargetCensus is the census a run takes against a live target.
func NewTargetCensus(metricsURL string) TargetCensus {
	if metricsURL == "" {
		return TargetCensus{}
	}
	return TargetCensus{Read: func() (TargetHealth, bool) {
		health, err := FetchTargetHealth(metricsURL)
		return health, err == nil
	}}
}

// CensusReading is what one census came back with.
//
// The two counts travel with the wait that produced them, because a reading the
// target needed seven seconds to give is a different fact from the same number
// answered at once — and only the second is a target keeping up with its own
// fleet.
type CensusReading struct {
	// Agents is the fleet the target says it is holding, and Goroutines the
	// count that bounds it below. Both absent where the question had no answer.
	Agents     *int
	Goroutines *float64

	// Waited is how long the run held still while the target admitted machines
	// it had already accepted. Zero where the first answer already accounted
	// for the fleet, which is every healthy leg.
	Waited time.Duration

	// Absent is why there are no counts, where the run can say.
	Absent string
}

// Take reads the target's two counts, holding still while the target is still
// admitting machines the run has already counted.
//
// Both counts come from one reading rather than two, so the pair describes one
// instant of the target the way the phase's own count describes one instant of
// the fleet.
//
// floor is the fewest machines the run can have been holding, asked again at
// every reading because it falls as machines leave. A fleet that is losing
// machines under the question — which is the soak's ordinary condition, since
// it replaces every machine it loses — would otherwise be waited on for a
// catch-up that can never happen.
func (c TargetCensus) Take(floor func() int, clock Clock) CensusReading {
	if c.Read == nil {
		return CensusReading{}
	}

	// The first question is asked whatever the answer, so the clock starts
	// after it: what this measures is the holding still, and a phase whose
	// target answered for the whole fleet held still for no time at all.
	//
	// It is the clock rather than a count of the intervals slept, because
	// reading the target's page is itself something a loaded target is slow at:
	// the read is given fifteen seconds of its own, so a budget spent in
	// questions asked would hold a phase open for a quarter of an hour on
	// exactly the target already struggling to answer.
	reading := c.read()
	began := clock.Now()
	waited := time.Duration(0)
	for reading.Absent == "" && *reading.Agents < floor() && waited < censusSettleLimit {
		clock.Sleep(censusSettleInterval)
		// A target that answered and then stopped answering leaves the run with
		// a number it can no longer bracket, so the census says the target went
		// quiet rather than reporting a fleet it half-heard.
		reading = c.read()
		waited = clock.Now().Sub(began)
	}
	reading.Waited = waited
	return reading
}

// read is one reading of the target's page, or why there is none.
func (c TargetCensus) read() CensusReading {
	health, answered := c.Read()
	if !answered {
		return CensusReading{Absent: censusAbsentTargetSilent}
	}
	if health.AgentsConnected == nil {
		return CensusReading{Absent: censusAbsentNoFleetCount}
	}

	held := int(*health.AgentsConnected)
	running := health.Goroutines
	return CensusReading{Agents: &held, Goroutines: &running}
}

// TargetReading is everything a phase asks the target about itself: how hard it
// worked, and what it says it is holding.
//
// They travel together because they are two readings off one exposition page,
// taken at the same boundary, and a run that could take either could take both
// — which is what makes an absence in one of them a reading somebody dropped
// rather than a venue that publishes nothing.
type TargetReading struct {
	Busy   TargetBusy
	Census TargetCensus
}
