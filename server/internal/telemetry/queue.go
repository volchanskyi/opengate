package telemetry

import (
	"sync"
	"time"
)

// entry is one hand-off waiting in a class's queue.
type entry struct {
	sub      Submission
	readings encodedReadings
	received time.Time
}

func (e *entry) size() int { return len(e.readings.body) }

// queue holds one class's encoded readings. Its bound counts what waits and what is in flight,
// so a slow store holds the class to its bound.
type queue struct {
	class Class
	bound int
	obs   WriterObserver

	mu       sync.Mutex
	entries  []*entry
	queued   int
	inflight int
	closed   bool

	// wake carries a signal that something was queued; it holds one so a push never blocks.
	wake chan struct{}
}

func newQueue(class Class, bound int, obs WriterObserver) *queue {
	return &queue{class: class, bound: bound, obs: obs, wake: make(chan struct{}, 1)}
}

// push queues e, refusing it when the class is closed or e does not fit inside the bound.
func (q *queue) push(e *entry) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrShuttingDown
	}
	if q.queued+q.inflight+e.size() > q.bound {
		return ErrQueueFull
	}
	q.entries = append(q.entries, e)
	q.queued += e.size()
	q.report(e.received)
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// due reports how much is queued and how long the oldest entry has waited.
func (q *queue) due(now time.Time) (queued int, oldest time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) == 0 {
		return 0, 0
	}
	return q.queued, now.Sub(q.entries[0].received)
}

// take hands every queued entry to the sender, which then holds them as in flight.
func (q *queue) take(now time.Time) []*entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	batch := q.entries
	q.entries = nil
	q.inflight += q.queued
	q.queued = 0
	q.report(now)
	return batch
}

// finish gives back the bytes of entries whose request has been answered.
func (q *queue) finish(bytes int, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inflight -= bytes
	q.report(now)
}

// close refuses every later push.
func (q *queue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
}

// fill is the share of the bound held, queued and in flight together.
func (q *queue) fill() float64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return float64(q.queued+q.inflight) / float64(q.bound)
}

// report publishes what the class holds; the caller holds the lock.
func (q *queue) report(now time.Time) {
	if q.obs == nil {
		return
	}
	var oldest time.Duration
	if len(q.entries) > 0 {
		oldest = now.Sub(q.entries[0].received)
	}
	q.obs.Held(q.class.String(), q.queued+q.inflight, oldest)
}
