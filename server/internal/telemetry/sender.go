package telemetry

import (
	"bytes"
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
		w.settle(q, e, ErrShuttingDown)
		q.finish(e.size(), now)
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

// sendOnce writes everything queued in one request. Each entry's write is registered before its
// machine is checked against the deny-list, so an erasure waits for any check that passed.
func (w *Writer) sendOnce(q *queue) {
	now := time.Now()
	batch := q.take(now)
	if len(batch) == 0 {
		return
	}
	var body bytes.Buffer
	total := 0
	for _, e := range batch {
		total += e.size()
	}
	// The request is sized once to the batch, so a multi-megabyte catch-up body never doubles.
	body.Grow(total)
	sending := make([]*entry, 0, len(batch))
	tokens := make([]*writeToken, 0, len(batch))
	for _, e := range batch {
		if q.class == ClassCatchUp && now.Sub(e.received) > w.cfg.QueueWait {
			w.settle(q, e, ErrWaitedTooLong)
			q.finish(e.size(), now)
			continue
		}
		token := w.writes.register(e.sub.Scope)
		if w.cfg.Tombstoned != nil && w.cfg.Tombstoned(e.sub.Scope.Device) {
			w.writes.finish(token)
			w.settle(q, e, ErrTombstoned)
			q.finish(e.size(), now)
			continue
		}
		body.Write(e.readings.body)
		sending = append(sending, e)
		tokens = append(tokens, token)
	}
	if len(sending) == 0 {
		return
	}

	started := time.Now()
	reqCtx, cancel := context.WithTimeout(context.Background(), w.cfg.WriteTimeout)
	err := w.cfg.Store.Import(reqCtx, body.Bytes())
	cancel()
	if w.cfg.Observer != nil {
		w.cfg.Observer.Request(q.class.String(), body.Len(), time.Since(started), err)
	}
	if err != nil {
		err = fmt.Errorf("%w: %w", ErrStoreWrite, err)
	}
	answered := time.Now()
	for i, e := range sending {
		w.writes.finish(tokens[i])
		w.settle(q, e, err)
		q.finish(e.size(), answered)
	}
}
