# WS-19 — Declarative edge threshold alert rules

**Objective:** Add user-defined, edge-evaluated **threshold** alerts (e.g., disk < X %, CPU > Y %
sustained for N s) alongside the WS-2 ML anomaly detection, evaluated locally every window so a
breach is flagged in ~1 s — no central polling cycle.

**Dependencies:** WS-2 (sampler/ensemble + window cadence). **Extends:** WS-15b (dashboards/soak).
**Wave:** small; with WS-13/WS-12.

## Context

WS-2 already does 1 s edge sampling + ML anomaly. Netdata pairs ML with **declarative threshold
alarms**. This WS adds per-tenant threshold **rules** evaluated at the edge; **delivery stays
investigation-aid only** (no auto-notify) per the master-plan posture until the FPR soak.

## File inventory

- **Create:** `mesh-agent-core/src/alerts/` — a declarative rule evaluator (metric, comparator,
  threshold, sustain duration, hysteresis) over the sampler dims; emits a breach signal.
- **Modify:** [`control.rs`](../../../agent/crates/mesh-protocol/src/control.rs) / Go protocol — carry
  breach state in `AgentHealthSummary` (reuse; additive field) rather than a new message where
  possible; capability-gated; goldens.
- **Modify:** rule config delivery — rules are tenant-scoped config pushed to the agent (server→agent,
  capability-gated) or shipped in agent config; default ruleset minimal.
- **Modify:** [`deploy/grafana/provisioning/dashboards/`](../../../deploy/grafana/provisioning/dashboards/) —
  surface breach counts (WS-15b dashboard).

## Steps (TDD-first)

1. **Test first:** evaluator tests — a sustained breach fires after N s, clears with hysteresis,
   flapping is suppressed; positive + negative → implement.
2. **Test first (cross-lang):** breach state round-trips in `AgentHealthSummary`; capability-gated;
   `make golden`.
3. **Test first:** rule-config plumbing is tenant-scoped (org A's rules never reach org B) → implement.
4. Surface breach counts on the WS-15b dashboard.

## Gotchas / constraints

- **Investigation-aid only** — no auto-notify until the FPR soak (WS-15b posture); breaches are signals.
- Hysteresis + sustain duration to avoid flapping; bounded rule count per tenant.
- Reuse `AgentHealthSummary` (additive) — avoid a new message/QUIC stream; respect WS-3 caps.

## Reviewer checklist

- [ ] Evaluator: sustain + hysteresis + flap suppression; positive + negative tested.
- [ ] Breach state additive in `AgentHealthSummary`; capability-gated; goldens green.
- [ ] Rule config tenant-scoped; default ruleset minimal; no auto-notify; `/precommit` green.

## Verification

`cd agent && cargo test -p mesh-agent-core`; `make golden`; `cd server && go test ./internal/...`.
`/precommit` green. `/docs`: Monitoring page.
