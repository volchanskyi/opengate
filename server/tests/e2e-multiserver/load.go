package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"nhooyr.io/websocket"
)

// loadBaseline measures steady-state one-way relay latency for two routing modes
// — "direct" (both sides on replica B, zero-hop) and "proxied" (agent on A,
// browser on B, spliced through the affinity owner) — and prints a p50/p95/p99
// table plus the proxied-vs-direct delta. The delta quantifies the
// extra intra-cluster hop the cross-server proxy adds.
func loadBaseline(ctx context.Context, h *harness, samples int) error {
	direct, err := h.measureMode(ctx, "direct", samples)
	if err != nil {
		return fmt.Errorf("direct mode: %w", err)
	}
	proxied, err := h.measureMode(ctx, "proxied", samples)
	if err != nil {
		return fmt.Errorf("proxied mode: %w", err)
	}

	h.logf("=== relay one-way latency baseline (%d samples each) ===", samples)
	reportLatency(h.logf, "direct  (same replica)", direct)
	reportLatency(h.logf, "proxied (cross replica)", proxied)
	h.logf("proxied-vs-direct delta: p50 %+v  p99 %+v",
		(percentile(proxied, 50) - percentile(direct, 50)).Round(time.Microsecond),
		(percentile(proxied, 99) - percentile(direct, 99)).Round(time.Microsecond))
	return nil
}

// measureMode opens one session in the requested routing mode and times `samples`
// one-way frames (agent→browser) over the established pair, returning the latency
// sample set. Measuring both conns from this single process means t0 and t1 share
// a clock — no skew to correct for.
func (h *harness) measureMode(ctx context.Context, mode string, samples int) ([]time.Duration, error) {
	token, err := h.seedSession(ctx)
	if err != nil {
		return nil, err
	}
	agentURL := h.serverB
	if mode == "proxied" {
		agentURL = h.serverA // split the sides across replicas to force the proxy
	}

	dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	agent, err := h.dialRelay(dctx, agentURL, token, "agent")
	if err != nil {
		return nil, err
	}
	defer agent.Close(websocket.StatusNormalClosure, "")
	browser, err := h.dialRelay(dctx, h.serverB, token, "browser")
	if err != nil {
		return nil, err
	}
	defer browser.Close(websocket.StatusNormalClosure, "")

	// One warm-up frame lets the cross-server splice finish before timing.
	if err := exchange(dctx, agent, browser, []byte("warmup")); err != nil {
		return nil, fmt.Errorf("warmup: %w", err)
	}

	latencies := make([]time.Duration, 0, samples)
	payload := fmt.Appendf(nil, "%s-sample", mode)
	for range samples {
		t0 := time.Now()
		if err := exchange(dctx, agent, browser, payload); err != nil {
			return nil, err
		}
		latencies = append(latencies, time.Since(t0))
	}
	return latencies, nil
}

// reportLatency prints p50/p95/p99 for a labelled sample set.
func reportLatency(logf func(string, ...any), label string, samples []time.Duration) {
	logf("  %-24s p50=%v  p95=%v  p99=%v", label,
		percentile(samples, 50).Round(time.Microsecond),
		percentile(samples, 95).Round(time.Microsecond),
		percentile(samples, 99).Round(time.Microsecond))
}

// percentile returns the pth-percentile duration (nearest-rank) of samples.
func percentile(samples []time.Duration, p int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
