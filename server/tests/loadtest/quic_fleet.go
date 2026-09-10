package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

// StartAgent is one machine's whole life. It reports its own arrival — the
// moment it is connected, handshook and registered — by calling noteArrival,
// and returns when the context is cancelled, or earlier if the machine could
// not connect at all.
//
// The arrival is signalled where it happens rather than read off the result,
// because a phase is a window in time and a machine that arrives inside one is
// held long past its end. Counting arrivals at the return counts them in
// whichever phase the machine's life happened to end in — which for a fleet
// held to the end of the walk is no phase at all, so every phase of every
// profiled run reported no arrivals against an offer it had met.
type StartAgent func(ctx context.Context, index int, noteArrival func()) agentResult

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
// winding them down as needed. The window is the time it has to get there.
//
// The climb is spread across that window rather than dialled at once, and the
// difference is the difference between an offer and a burst. A phase going from
// eight thousand machines to sixteen thousand over five minutes is offering
// fifty-three arrivals a second, which is a rate the server accepts; the same
// machines dialled the instant the step is asked for are sixteen hundred
// arrivals in a moment, which is a rate it refuses on purpose. The refusals then
// read as machines that could not arrive, on a target that was never asked to
// carry them.
//
// Winding down is not paced. A machine leaving is not an arrival, and nothing
// on the other end rations departures.
func (f *QUICFleet) HoldConnected(within time.Duration, target int) error {
	if target < 0 {
		target = 0
	}

	// The level the run last asked for, which is not the same as how many
	// machines are connected: the difference between those two is the finding,
	// and topping it back up would be the harness quietly closing it.
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

// climb brings up count machines, each one taking its turn inside the window.
//
// Every machine takes its place in the level immediately, so the step after this
// one asks for the level it was going to ask for; only the dialling waits. A
// window of nothing is every machine at once, which is what a fleet with no time
// to spread over should do.
func (f *QUICFleet) climb(count int, within time.Duration) {
	spacing := time.Duration(0)
	if within > 0 {
		spacing = within / time.Duration(count)
	}
	for i := 0; i < count; i++ {
		// The last machine dials at the end of the window rather than the first
		// one dialling at its start, so the level is reached when the window
		// says and no machine arrives before the step that asked for it.
		f.startOne(spacing * time.Duration(i+1))
	}
}

// waitOut spends d and reports whether it got all the way there.
//
// A machine the run winds down while it is still waiting its turn never dials,
// and that is the point: it offered nothing and saw nothing, so it is neither an
// arrival nor one that failed to arrive.
func waitOut(ctx context.Context, d time.Duration) bool {
	// A machine with no turn to wait for goes straight to dialling, whatever the
	// run has since decided. Its dial is what deals with a context already
	// ended, and that is the same machine an unpaced fleet has always started.
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
			// Let go before its turn came. stopOne has already taken it out of
			// the level, and it has nothing to report either way.
			return
		}
		result := f.start(ctx, index, func() { f.noteArrival(&arrived) })

		f.mu.Lock()
		f.results = append(f.results, result)
		f.tallyLocked(result, arrived.Load())
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

// noteArrival counts one machine reaching registered, once.
//
// A machine that comes back after an outage is the same machine returning,
// which persistThrough counts as a reconnection rather than as a second
// arrival — so the flag it is given is what decides, not the number of times
// the machine said so.
func (f *QUICFleet) noteArrival(arrived *atomic.Bool) {
	if !arrived.CompareAndSwap(false, true) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes.Arrived++
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

// tallyLocked records what became of one machine, given whether it ever
// arrived. The caller holds the lock.
//
// A machine that never reached registered is the failure, and it is counted
// here because its life ending is the first moment anything knows. One that did
// arrive and was later cut off is a fault instead: it turned up, so counting it
// as a failure to turn up puts one machine into the attempted tally twice and
// reports an error rate for a phase whose every machine arrived.
//
// A refusal the server made on purpose is held apart from both: counting a
// correctly enforced limit as a defect makes the limit look broken and buries
// the real failures underneath it.
func (f *QUICFleet) tallyLocked(result agentResult, arrived bool) {
	if !arrived {
		f.outcomes.Failed++
	}
	if result.err == nil {
		return
	}
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
