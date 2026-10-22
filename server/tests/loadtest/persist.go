package main

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"
)

// redialBase and redialCap bound the full-jitter backoff window a simulated machine redials in.
const (
	redialBase = 1 * time.Second
	redialCap  = 30 * time.Second
)

// persistThrough redials a persistent machine until the run winds down, keeping its first arrival,
// and gives any other machine one more dial when its first never arrived.
func persistThrough(ctx context.Context, opts loadOptions, live func(context.Context) agentResult) agentResult {
	res := live(ctx)
	if !opts.reconnect {
		if res.err == nil || !res.arrivedAt.IsZero() {
			return res
		}
		select {
		case <-ctx.Done():
			return res
		case <-time.After(redialDelay(0)):
		}
		again := live(ctx)
		again.retried = 1
		return again
	}

	for attempt := 0; res.err != nil && ctx.Err() == nil; attempt++ {
		select {
		case <-ctx.Done():
			return res
		case <-time.After(redialDelay(attempt)):
		}

		again := live(ctx)
		res.redials++
		res.err = again.err
		if !again.arrivedAt.IsZero() {
			res.reconnected++
		}
	}
	return res
}

// redialDelay is one full-jitter wait, uniform up to a window that doubles
// from the base to the cap.
func redialDelay(attempt int) time.Duration {
	window := redialCap
	if attempt < 30 {
		if scaled := redialBase << attempt; scaled > 0 && scaled < redialCap {
			window = scaled
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(window)+1))
	if err != nil {
		// A failed random read waits the whole window, which delays the redial.
		return window
	}
	return time.Duration(n.Int64())
}
