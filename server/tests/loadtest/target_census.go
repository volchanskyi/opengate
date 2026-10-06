package main

import "time"

// Reasons a census is absent; a phase naming one keeps its place in the bundle.
const (
	censusAbsentTargetSilent = "the target did not answer when the phase closed"
	censusAbsentNoFleetCount = "the target published no count of the fleet it holds"
)

const (
	// censusSettleInterval is how often the run asks again while the target is still behind.
	censusSettleInterval = 500 * time.Millisecond

	// censusSettleLimit bounds how long the run holds still while the target admits machines.
	// It bounds the wait only; a shortfall that outlasts it still counts.
	censusSettleLimit = 180 * time.Second
)

// TargetCensus asks the target what it is holding; a run with no target asks nothing.
type TargetCensus struct {
	// Read returns one reading of the target's page and whether it answered.
	Read func() (TargetHealth, bool)
}

// NewTargetCensus reads a live target; an empty address reads nothing.
func NewTargetCensus(metricsURL string) TargetCensus {
	if metricsURL == "" {
		return TargetCensus{}
	}
	return TargetCensus{Read: func() (TargetHealth, bool) {
		health, err := FetchTargetHealth(metricsURL)
		return health, err == nil
	}}
}

// CensusReading is what one census returned, with the wait that produced it.
type CensusReading struct {
	// Agents is the fleet the target reports holding and Goroutines the count bounding it below.
	// Both are nil where the question had no answer.
	Agents     *int
	Goroutines *float64

	// ResidentBytes is the target's resident memory at the same reading.
	ResidentBytes *float64

	// Waited is how long the run held still while the target admitted accepted machines.
	Waited time.Duration

	// Absent is why there are no counts, where the run can say.
	Absent string
}

// Take reads the target's counts, waiting while the target reports fewer agents than floor().
// floor is asked again at every reading because the run's own count falls as machines leave.
func (c TargetCensus) Take(floor func() int, clock Clock) CensusReading {
	if c.Read == nil {
		return CensusReading{}
	}

	// The clock starts after the first reading and bounds the wait, since a loaded target reads slowly.
	reading := c.read()
	began := clock.Now()
	waited := time.Duration(0)
	for reading.Absent == "" && *reading.Agents < floor() && waited < censusSettleLimit {
		clock.Sleep(censusSettleInterval)
		// A target that stops answering mid-wait reports as absent.
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
	resident := health.ResidentBytes
	return CensusReading{Agents: &held, Goroutines: &running, ResidentBytes: &resident}
}

// PhaseReadings is everything a phase reads at its boundaries: target busy-ness and census,
// generator room, and dropped datagrams.
type PhaseReadings struct {
	Busy      TargetBusy
	Census    TargetCensus
	Generator GeneratorRoom
	Network   NetworkDrops
}
