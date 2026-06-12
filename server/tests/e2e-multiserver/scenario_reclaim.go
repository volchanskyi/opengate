package main

import (
	"context"
	"fmt"
	"time"
)

// scenarioOwnerDeathReclaim proves that when the affinity owner dies, a new pair
// for the same token reclaims on the surviving replica once the stale affinity
// claim expires (OPENGATE_AFFINITY_TTL). There is no live migration; clients
// reconnect with a fresh token. Here the token is reused to exercise the TTL
// reclaim path specifically.
func scenarioOwnerDeathReclaim(ctx context.Context, h *harness) error {
	token, err := h.seedSession(ctx)
	if err != nil {
		return err
	}

	// Establish A as the affinity owner (agent connects to A first), then prove
	// the cross-server pair is live before killing A.
	setupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	agent, browser, closePair, err := h.dialPair(setupCtx, h.serverA, h.serverB, token)
	if err != nil {
		return err
	}
	if err := exchange(setupCtx, agent, browser, []byte("pre-kill")); err != nil {
		closePair()
		return fmt.Errorf("pre-kill exchange: %w", err)
	}

	// SIGKILL the owner while the agent is still connected, so A never runs its
	// teardown and its affinity claim survives in Redis until the TTL lapses.
	// Closing our local handles first would let A release the claim gracefully.
	if err := h.composeKill("server-a"); err != nil {
		closePair()
		return fmt.Errorf("kill owner: %w", err)
	}
	closePair()                                       // dead conns; B tears down its proxy side
	defer func() { _ = h.composeStart("server-a") }() // revive for stack teardown hygiene

	// Reconnect the same token to the surviving replica B. While A's affinity
	// claim is still live, B proxies to the dead A and the dial fails; once the
	// claim TTL lapses, B reclaims ownership and pairs locally.
	if err := h.reclaimOnServerB(ctx, token); err != nil {
		return err
	}
	h.logf("  session reclaimed on surviving replica after owner death")
	return nil
}

// reclaimOnServerB retries a same-token pair against replica B until a frame
// round-trips (reclaim succeeded) or the deadline passes.
func (h *harness) reclaimOnServerB(ctx context.Context, token string) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := h.tryPairOnServerB(ctx, token); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("session did not reclaim on surviving replica within deadline: %w", lastErr)
}

// tryPairOnServerB attempts one same-server pair on replica B and a single frame
// exchange, closing both conns before returning.
func (h *harness) tryPairOnServerB(ctx context.Context, token string) error {
	attemptCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	// B's proxy teardown (and each failed proxy attempt) deletes the session row;
	// re-create it before every attempt so the relay token check passes.
	if err := h.ensureSessionRow(attemptCtx, token); err != nil {
		return err
	}
	agent, browser, closePair, err := h.dialPair(attemptCtx, h.serverB, h.serverB, token)
	if err != nil {
		return err
	}
	defer closePair()
	return exchange(attemptCtx, agent, browser, []byte("post-reclaim"))
}
