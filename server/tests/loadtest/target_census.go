package main

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

// Take reads the target's two counts now, or says why neither is there.
//
// Both come from one reading rather than two, so the pair describes one instant
// of the target the way the phase's own count describes one instant of the
// fleet.
func (c TargetCensus) Take() (agents *int, goroutines *float64, absent string) {
	if c.Read == nil {
		return nil, nil, ""
	}

	health, answered := c.Read()
	if !answered {
		return nil, nil, censusAbsentTargetSilent
	}
	if health.AgentsConnected == nil {
		return nil, nil, censusAbsentNoFleetCount
	}

	held := int(*health.AgentsConnected)
	running := health.Goroutines
	return &held, &running, ""
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
