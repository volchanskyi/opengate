package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

// A fleet that can be asked to hold a level.
//
// The sequencer walks phases and says how many machines should be connected at
// each step. Something has to make that true, and it has to do it the way an
// estate behaves: a machine that is already connected stays connected, a new one
// joins beside it, and a machine the run winds down closes deliberately rather
// than being counted as one the server dropped.
//
// The dialling itself is handed in. What is worth testing here is the
// bookkeeping — who is up, who never arrived, and what each one's timings were —
// and that is exercised without a server on the other end.

// StartAgent is one machine's whole life. It returns when the context is
// cancelled, or earlier if the machine could not connect at all.
type StartAgent func(ctx context.Context, index int) agentResult

// ProbeRoundTrip dials one machine, takes it all the way to registered, and
// hangs up — reporting how long that took.
//
// It is a whole arrival rather than a message on a connection already open
// because the control stream has no reply to a heartbeat: there is nothing on
// the machine side to time a round trip against except making a new one.
type ProbeRoundTrip func(ctx context.Context) (time.Duration, error)

// QUICFleet holds a number of machines connected.
type QUICFleet struct {
	start StartAgent
	probe ProbeRoundTrip

	mu sync.Mutex
	// running is keyed by the machine's own index, so a machine that never
	// arrived is removed exactly rather than by dropping whichever entry
	// happens to be last.
	running map[int]context.CancelFunc
	// order is the level: every machine the run has asked for and not yet wound
	// down, in the order it was asked for. A machine that never arrived keeps
	// its place here after leaving running, which is what stops the next step
	// of a ramp from dialling a replacement for it.
	order   []int
	next    int
	results []agentResult
	// outcomes is what the machines have seen, tallied as each one ends. It is
	// cumulative because a phase is the difference between two readings of it.
	outcomes FleetOutcomes

	wg sync.WaitGroup
}

// NewQUICFleet builds a fleet that starts machines with the given function and
// takes no round trips of its own.
func NewQUICFleet(start StartAgent) *QUICFleet {
	return NewQUICFleetWithProbe(start, nil)
}

// NewQUICFleetWithProbe builds a fleet that can also take a live round trip
// while it holds its level.
func NewQUICFleetWithProbe(start StartAgent, probe ProbeRoundTrip) *QUICFleet {
	return &QUICFleet{start: start, probe: probe, running: map[int]context.CancelFunc{}}
}

// HoldConnected brings the fleet to the level asked for, adding machines or
// winding them down as needed.
func (f *QUICFleet) HoldConnected(_ time.Duration, target int) error {
	if target < 0 {
		target = 0
	}

	// The level the run last asked for, which is not the same as how many
	// machines are connected: the difference between those two is the finding,
	// and topping it back up would be the harness quietly closing it.
	f.mu.Lock()
	current := len(f.order)
	f.mu.Unlock()

	for i := current; i < target; i++ {
		f.startOne()
	}
	for i := current; i > target; i-- {
		f.stopOne()
	}
	return nil
}

// startOne brings up a single machine and records it when it ends.
func (f *QUICFleet) startOne() {
	ctx, cancel := context.WithCancel(context.Background())

	f.mu.Lock()
	index := f.next
	f.next++
	f.running[index] = cancel
	f.order = append(f.order, index)
	f.mu.Unlock()

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		result := f.start(ctx, index)

		f.mu.Lock()
		f.results = append(f.results, result)
		f.tallyLocked(result)
		// A machine that has ended is not one of the connected, whichever way it
		// ended. Removing only the ones that errored made the count a count of
		// machines started: a machine that finished its hold normally stayed
		// counted for the life of the run, and a bundle reporting five hundred
		// connected was reporting five hundred once dialled.
		//
		// It keeps its place in the level so that the rest of the ramp asks for
		// the level it was going to ask for anyway: a fleet that replaced it
		// would dial again at every remaining step, report the same outcome once
		// per step under a new machine each time, and end a phase having tried
		// some number of machines that is a property of the scheduler rather
		// than of the profile.
		delete(f.running, index)
		f.mu.Unlock()
		cancel()
	}()
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

// forgetLocked drops one machine from the level and from the connected. The
// caller holds the lock. A machine that never arrived has already left the
// connected, and winding the level down past it still has to take it out of the
// level — otherwise the subtraction lands on a machine that is carrying load.
func (f *QUICFleet) forgetLocked(index int) {
	delete(f.running, index)
	for i, candidate := range f.order {
		if candidate == index {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
}

// tallyLocked records one machine's outcome. The caller holds the lock.
//
// A refusal the server made on purpose is held apart from a fault: counting a
// correctly enforced limit as a defect makes the limit look broken and buries
// the real failures underneath it.
func (f *QUICFleet) tallyLocked(result agentResult) {
	if result.err == nil {
		f.outcomes.Arrived++
		return
	}
	f.outcomes.Failed++
	if errors.Is(result.err, ErrHeldPeerGone) {
		f.outcomes.Severed++
	}
	if errors.Is(result.err, ErrEnrollmentRefused) {
		f.outcomes.Rejected++
	}
}

// Connected is how many machines are actually up.
func (f *QUICFleet) Connected() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.running)
}

// Outcomes is what this fleet's machines have seen so far.
func (f *QUICFleet) Outcomes() FleetOutcomes {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outcomes
}

// probeBudget bounds one round trip, so a phase whose target has stopped
// answering is not held up by the measurement of it.
const probeBudget = 30 * time.Second

// ProbeLatency takes a live round trip now.
//
// A fleet with no prober, or one whose round trip could not be taken, reports
// zero — which the phase reads as no sample rather than as an instant one.
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

// Stop winds the whole fleet down and waits for every machine to finish, so a
// run that has ended is not still holding connections open.
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
