package main

import (
	"context"
	"fmt"
	"time"

	"nhooyr.io/websocket"
)

// scenarioRedisDeathDegraded proves the ADR-023 recovery posture on Redis loss:
// the registry_up gauge flips to 0 (the Grafana/Telegram alert keys on this), an
// already-established session keeps relaying (drains, not dropped), new sessions
// are refused with WebSocket 1013 (Try Again Later), and the system recovers when
// Redis returns. Everything runs on replica B (replica A may be reviving from the
// prior scenario).
func scenarioRedisDeathDegraded(ctx context.Context, h *harness) error {
	base := h.serverB
	if err := h.waitRegistry(ctx, base, true, 15*time.Second); err != nil {
		return fmt.Errorf("precondition (registry up): %w", err)
	}

	token, err := h.seedSession(ctx)
	if err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	agent, browser, closePair, err := h.dialPair(sctx, base, base, token)
	if err != nil {
		return err
	}
	defer closePair()
	if err := exchange(sctx, agent, browser, []byte("pre-redis-kill")); err != nil {
		return fmt.Errorf("pre-kill exchange: %w", err)
	}

	if err := h.composeStop("redis"); err != nil {
		return fmt.Errorf("kill redis: %w", err)
	}
	redisRestored := false
	defer func() {
		if !redisRestored {
			_ = h.composeStart("redis")
		}
	}()

	// 1) gauge flips to 0 (alert signal).
	if err := h.waitRegistry(ctx, base, false, 25*time.Second); err != nil {
		return fmt.Errorf("registry_up did not flip to 0: %w", err)
	}
	h.logf("  registry_up flipped to 0 on Redis loss")

	// 2) in-flight session keeps relaying (drains).
	if err := exchange(sctx, agent, browser, []byte("in-flight-drains")); err != nil {
		return fmt.Errorf("in-flight session dropped during outage: %w", err)
	}
	h.logf("  in-flight session kept relaying during outage")

	// 3) new sessions refused with 1013 once degraded mode trips.
	if err := h.expectDegradedRefusal(ctx, base, 25*time.Second); err != nil {
		return err
	}
	h.logf("  new session refused with WS 1013 (Try Again Later)")

	// 4) recovery.
	if err := h.composeStart("redis"); err != nil {
		return fmt.Errorf("restart redis: %w", err)
	}
	redisRestored = true
	if err := h.waitRegistry(ctx, base, true, 25*time.Second); err != nil {
		return fmt.Errorf("registry_up did not recover: %w", err)
	}
	if err := h.assertFreshSessionWorks(ctx, base); err != nil {
		return fmt.Errorf("post-recovery session failed: %w", err)
	}
	h.logf("  registry recovered; fresh sessions accepted again")
	return nil
}

// expectDegradedRefusal polls (seed → browser dial → read) until the relay closes
// a new session with 1013, proving degraded mode refuses new work. Before the
// degraded threshold lapses the relay may still pair locally (resolveOwner falls
// back to self on a claim error), so transient successes are retried.
func (h *harness) expectDegradedRefusal(ctx context.Context, base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		token, err := h.seedSession(ctx)
		if err != nil {
			return err
		}
		if code, err := h.probeBrowserClose(ctx, base, token); err == nil && code == websocket.StatusTryAgainLater {
			return nil
		} else if err != nil {
			last = err
		} else {
			last = fmt.Errorf("expected close 1013, got %d", code)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("did not observe 1013 refusal within %s: %w", timeout, last)
}

// probeBrowserClose dials a browser side and returns the close code observed on
// the first read (StatusTryAgainLater when degraded).
func (h *harness) probeBrowserClose(ctx context.Context, base, token string) (websocket.StatusCode, error) {
	dctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	c, err := h.dialRelay(dctx, base, token, "browser")
	if err != nil {
		return 0, err
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	_, _, readErr := c.Read(dctx)
	return websocket.CloseStatus(readErr), nil
}

// assertFreshSessionWorks seeds a new same-server pair on base and asserts a frame
// round-trips, confirming recovery.
func (h *harness) assertFreshSessionWorks(ctx context.Context, base string) error {
	token, err := h.seedSession(ctx)
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	agent, browser, closePair, err := h.dialPair(dctx, base, base, token)
	if err != nil {
		return err
	}
	defer closePair()
	return exchange(dctx, agent, browser, []byte("post-recovery"))
}
