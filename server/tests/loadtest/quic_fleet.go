package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// fleetPresence reports attachment per connection, so a machine that reconnects attaches again.
type fleetPresence struct {
	// Arrived fires when a connection registers.
	Arrived func()
	// Left fires when a registered connection ends.
	Left func()
}

// StartAgent runs one machine until its context ends or it fails to connect, signalling presence.
type StartAgent func(ctx context.Context, index int, presence fleetPresence) agentResult

// ProbeRoundTrip dials one machine to registered, hangs up and returns the elapsed time.
type ProbeRoundTrip func(ctx context.Context) (time.Duration, error)

// QUICFleet holds a number of machines connected.
type QUICFleet struct {
	start StartAgent
	probe ProbeRoundTrip

	mu sync.Mutex
	// running is keyed by machine index so a machine that never arrived is removed exactly.
	running map[int]context.CancelFunc
	// order is the level: every machine asked for and not wound down, in request order.
	order []int
	// connected counts machines that arrived and have not ended, the population the server counts.
	connected int
	next      int
	results   []agentResult
	// outcomes accumulates what machines saw, since a phase is the difference of two readings.
	outcomes FleetOutcomes

	wg sync.WaitGroup
}

// NewQUICFleet builds a fleet that starts machines with the given function and takes no probes.
func NewQUICFleet(start StartAgent) *QUICFleet {
	return NewQUICFleetWithProbe(start, nil)
}

// NewQUICFleetWithProbe builds a fleet that can also take a live round trip.
func NewQUICFleetWithProbe(start StartAgent, probe ProbeRoundTrip) *QUICFleet {
	return &QUICFleet{start: start, probe: probe, running: map[int]context.CancelFunc{}}
}

// HoldConnected brings the fleet to target machines, spacing new arrivals across within.
// Wind-down is unpaced because departures are rationed by nothing.
func (f *QUICFleet) HoldConnected(within time.Duration, target int) error {
	if target < 0 {
		target = 0
	}

	// The level asked for differs from the connected count, and that gap is left open.
	f.mu.Lock()
	current := len(f.order)
	f.mu.Unlock()

	if adding := target - current; adding > 0 {
		f.climb(adding, within)
	}
	for i := current; i > target; i-- {
		f.stopOne()
	}
	return nil
}

// climb adds count machines to the level at once and spaces their dials across the window.
func (f *QUICFleet) climb(count int, within time.Duration) {
	spacing := time.Duration(0)
	if within > 0 {
		spacing = within / time.Duration(count)
	}
	for i := 0; i < count; i++ {
		// The last machine dials at the window's end, so the level is reached when the window closes.
		f.startOne(spacing * time.Duration(i+1))
	}
}

// waitOut sleeps d and reports whether the full wait elapsed before ctx ended.
func waitOut(ctx context.Context, d time.Duration) bool {
	// A zero wait dials immediately; the dial itself handles an already-ended context.
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// startOne brings up a single machine after its turn comes round, and records it
// when it ends.
func (f *QUICFleet) startOne(after time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())

	f.mu.Lock()
	index := f.next
	f.next++
	f.running[index] = cancel
	f.order = append(f.order, index)
	f.mu.Unlock()

	f.wg.Add(1)
	var arrived atomic.Bool
	go func() {
		defer f.wg.Done()
		if !waitOut(ctx, after) {
			// stopOne already removed the machine from the level.
			return
		}
		result := f.start(ctx, index, fleetPresence{
			Arrived: func() { f.noteArrival(&arrived) },
			Left:    f.noteDeparture,
		})

		f.mu.Lock()
		f.results = append(f.results, result)
		f.tallyLocked(result, arrived.Load())
		// An ended machine leaves running but keeps its place in order, so the ramp dials no replacement.
		delete(f.running, index)
		f.mu.Unlock()
		cancel()
	}()
}

// noteArrival counts every registration as connected and only a machine's first as an arrival.
func (f *QUICFleet) noteArrival(arrived *atomic.Bool) {
	first := arrived.CompareAndSwap(false, true)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected++
	if first {
		f.outcomes.Arrived++
	}
}

// noteDeparture records one registered connection ending.
func (f *QUICFleet) noteDeparture() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected--
	f.outcomes.Departed++
}

// stopOne winds down the most recently started machine.
func (f *QUICFleet) stopOne() {
	f.mu.Lock()
	if len(f.order) == 0 {
		f.mu.Unlock()
		return
	}
	index := f.order[len(f.order)-1]
	cancel := f.running[index]
	f.forgetLocked(index)
	f.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// forgetLocked drops one machine from the level, so a wind-down past a machine that never
// arrived leaves the loaded ones alone. The caller holds the lock.
func (f *QUICFleet) forgetLocked(index int) {
	delete(f.running, index)
	for i, candidate := range f.order {
		if candidate == index {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
}

// tallyLocked classifies a machine's end by whether it arrived: stood down, refused on purpose,
// or failed to arrive; a later severed link is a fault. The caller holds the lock.
func (f *QUICFleet) tallyLocked(result agentResult, arrived bool) {
	switch {
	case arrived:
	case errors.Is(result.err, context.Canceled):
		f.outcomes.StoodDown++
	case errors.Is(result.err, ErrEnrollmentRefused):
		f.outcomes.Rejected++
	default:
		f.outcomes.Failed++
	}
	if result.err == nil {
		return
	}
	if errors.Is(result.err, ErrHeldPeerGone) {
		f.outcomes.Severed++
	}
}

// Connected counts machines that have arrived and not ended, the population the server counts.
func (f *QUICFleet) Connected() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

// Outcomes is what this fleet's machines have seen so far.
func (f *QUICFleet) Outcomes() FleetOutcomes {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outcomes
}

// probeBudget bounds one round trip so a silent target does not stall a phase.
const probeBudget = 30 * time.Second

// ProbeLatency takes a live round trip, returning zero when there is no prober or the trip fails.
func (f *QUICFleet) ProbeLatency() time.Duration {
	if f.probe == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeBudget)
	defer cancel()

	elapsed, err := f.probe(ctx)
	if err != nil {
		return 0
	}
	return elapsed
}

// Results is every machine's outcome, including the ones that never arrived.
func (f *QUICFleet) Results() []agentResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agentResult(nil), f.results...)
}

// Failures is the machines that could not connect.
func (f *QUICFleet) Failures() []agentResult {
	var failures []agentResult
	for _, result := range f.Results() {
		if result.err != nil {
			failures = append(failures, result)
		}
	}
	return failures
}

// Stop winds the whole fleet down and waits for every machine to finish.
func (f *QUICFleet) Stop() {
	f.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(f.running))
	for _, cancel := range f.running {
		cancels = append(cancels, cancel)
	}
	f.running = map[int]context.CancelFunc{}
	f.order = nil
	f.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	f.wg.Wait()
}
