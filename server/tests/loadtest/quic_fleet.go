package main

import (
	"context"
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

// QUICFleet holds a number of machines connected.
type QUICFleet struct {
	start StartAgent

	mu sync.Mutex
	// running is keyed by the machine's own index, so a machine that never
	// arrived is removed exactly rather than by dropping whichever entry
	// happens to be last.
	running map[int]context.CancelFunc
	order   []int
	next    int
	results []agentResult
	latency time.Duration

	wg sync.WaitGroup
}

// NewQUICFleet builds a fleet that starts machines with the given function.
func NewQUICFleet(start StartAgent) *QUICFleet {
	return &QUICFleet{start: start, running: map[int]context.CancelFunc{}}
}

// HoldConnected brings the fleet to the level asked for, adding machines or
// winding them down as needed.
func (f *QUICFleet) HoldConnected(_ time.Duration, target int) error {
	if target < 0 {
		target = 0
	}

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
		if result.err == nil && result.connectDur > 0 {
			f.latency = result.connectDur
		}
		if result.err != nil {
			// A machine that never connected is not one of the connected. It
			// leaves the level and stays in the results, because the gap between
			// what was asked for and what arrived is the finding.
			f.forgetLocked(index)
		}
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

// forgetLocked drops one machine from the level. The caller holds the lock.
func (f *QUICFleet) forgetLocked(index int) {
	if _, ok := f.running[index]; !ok {
		return
	}
	delete(f.running, index)
	for i, candidate := range f.order {
		if candidate == index {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
}

// Connected is how many machines are actually up.
func (f *QUICFleet) Connected() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.running)
}

// SampleLatency is the most recent connect time a machine reported, or zero
// before any has.
func (f *QUICFleet) SampleLatency() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.latency
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
