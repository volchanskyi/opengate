package telemetry

import (
	"context"
	"fmt"
	"time"
)

// run sends one class's requests until ctx ends, then drains for the shutdown grace and answers
// whatever is left with ErrShuttingDown.
func (w *Writer) run(ctx context.Context, q *queue) {
	for w.awaitFlush(ctx, q) {
		w.sendOnce(q)
	}
	q.close()
	deadline := time.Now().Add(w.cfg.ShutdownGrace)
	for time.Now().Before(deadline) {
		if queued, _ := q.due(time.Now()); queued == 0 {
			break
		}
		w.sendOnce(q)
	}
	now := time.Now()
	for _, e := range q.take(now) {
		w.answer(q, e, ErrShuttingDown, now)
	}
}

// awaitFlush waits until the queue reaches the flush size or its oldest entry the flush age,
// and reports false once ctx ends.
func (w *Writer) awaitFlush(ctx context.Context, q *queue) bool {
	for {
		queued, oldest := q.due(time.Now())
		if queued >= w.cfg.FlushBytes {
			return true
		}
		if queued > 0 && oldest >= w.cfg.FlushAge {
			return true
		}
		if ready, ended := w.sleepUntilDue(ctx, q, queued, oldest); ready || ended {
			return !ended
		}
	}
}

// sleepUntilDue waits for a push, the oldest entry's flush age, or ctx's end, whichever is first.
func (w *Writer) sleepUntilDue(ctx context.Context, q *queue, queued int, oldest time.Duration) (ready, ended bool) {
	var fire <-chan time.Time
	if queued > 0 {
		timer := time.NewTimer(w.cfg.FlushAge - oldest)
		defer timer.Stop()
		fire = timer.C
	}
	select {
	case <-ctx.Done():
		return false, true
	case <-q.wake:
		return false, false
	case <-fire:
		return true, false
	}
}

// sendOnce writes everything queued in one request read from the entries in place; each write is
// registered before the deny-list check, so an erasure waits for any check that passed.
func (w *Writer) sendOnce(q *queue) {
	now := time.Now()
	batch := q.take(now)
	if len(batch) == 0 {
		return
	}
	sending := make([]*entry, 0, len(batch))
	parts := make([][]byte, 0, len(batch))
	tokens := make([]*writeToken, 0, len(batch))
	total := 0
	for _, e := range batch {
		if q.class == ClassCatchUp && now.Sub(e.received) > w.cfg.QueueWait {
			w.answer(q, e, ErrWaitedTooLong, now)
			continue
		}
		token := w.writes.register(e.sub.Scope)
		if w.cfg.Tombstoned != nil && w.cfg.Tombstoned(e.sub.Scope.Device) {
			w.writes.finish(token)
			w.answer(q, e, ErrTombstoned, now)
			continue
		}
		sending = append(sending, e)
		parts = append(parts, e.readings.body)
		tokens = append(tokens, token)
		total += e.size()
	}
	if len(sending) == 0 {
		return
	}

	started := time.Now()
	reqCtx, cancel := context.WithTimeout(context.Background(), w.cfg.WriteTimeout)
	err := w.cfg.Store.Import(reqCtx, parts...)
	cancel()
	if w.cfg.Observer != nil {
		w.cfg.Observer.Request(q.class.String(), total, time.Since(started), err)
	}
	if err != nil {
		err = fmt.Errorf("%w: %w", ErrStoreWrite, err)
	}
	answered := time.Now()
	for i, e := range sending {
		w.writes.finish(tokens[i])
		w.answer(q, e, err, answered)
	}
}
