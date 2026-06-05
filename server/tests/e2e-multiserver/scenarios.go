package main

import (
	"context"
	"fmt"
	"time"
)

// scenario is one named end-to-end check. Each returns nil on success or a
// descriptive error; main runs them in order and reports a pass/fail table.
type scenario struct {
	name string
	run  func(ctx context.Context, h *harness) error
}

// scenarios returns the three ADR-023 multiserver checks in run order.
func scenarios() []scenario {
	return []scenario{
		{"cross-server-frame-flow", scenarioFrameFlow},
		{"owner-death-ttl-reclaim", scenarioOwnerDeathReclaim},
		{"redis-death-degraded-refuse", scenarioRedisDeathDegraded},
	}
}

// scenarioFrameFlow proves a session whose two sides land on different replicas
// relays bytes in both directions through the cross-server proxy (ADR-033): the
// agent connects to A, the browser to B, so exactly one side is always remote and
// spliced through the affinity owner's internal listener.
func scenarioFrameFlow(ctx context.Context, h *harness) error {
	token, err := h.seedSession(ctx)
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	agent, browser, closePair, err := h.dialPair(dctx, h.serverA, h.serverB, token)
	if err != nil {
		return err
	}
	defer closePair()

	const n = 20
	for i := range n {
		if err := exchange(dctx, agent, browser, fmt.Appendf(nil, "a2b-%04d", i)); err != nil {
			return fmt.Errorf("agent→browser frame %d: %w", i, err)
		}
		if err := exchange(dctx, browser, agent, fmt.Appendf(nil, "b2a-%04d", i)); err != nil {
			return fmt.Errorf("browser→agent frame %d: %w", i, err)
		}
	}
	h.logf("  %d frames relayed cross-server each direction", n)
	return nil
}
