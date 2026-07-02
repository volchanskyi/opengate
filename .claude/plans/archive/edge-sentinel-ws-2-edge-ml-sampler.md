# WS-2 — Edge ML kernel + host sampler (agent, clean-room, pure Rust)

**Objective:** Add a clean-room k=2 k-means ensemble that computes per-metric anomaly bits
locally, fed by a `sysinfo`-based sampler over a bounded ring buffer, with cmdline
redaction at source. No protocol/infra yet (logs locally, default-off).

**Dependencies:** none (agent-only). **Blocks:** WS-3 (message shapes). **Parallel with:**
WS-0, WS-1.

## Context

Agent is tokio-based; `sysinfo` is already a workspace dep, used on-demand in
`collect_hardware_info()` ([`main.rs:739`](../../../agent/crates/mesh-agent/src/main.rs#L739)).
There is no continuous sampling and no ML today. Netdata's design (clean-room target): k=2,
~18 staggered models/metric, **consensus** (all models agree) at the **99th-percentile**
training-distance boundary; lagged-window feature vectors catch shape, not just level.

## File inventory

- **Create:** `agent/crates/mesh-agent-core/src/ml/` — `mod.rs`, `kmeans.rs` (`KMeansModel`
  k=2), `ensemble.rs` (`EdgeMlEnsemble`, consensus), `window.rs` (`AnomalyRateWindow`,
  bit-packed), `sampler.rs` (`MetricSampler` trait + `SysinfoSampler` + `FakeSampler`),
  `redact.rs` (argv secret-redaction).
- **Modify:** [`agent/crates/mesh-agent/src/main.rs`](../../../agent/crates/mesh-agent/src/main.rs)
  (spawn a bounded background sampling task, default-off flag).
- **Reference:** bounded-channel precedent in
  [`session/mod.rs:84`](../../../agent/crates/mesh-agent-core/src/session/mod.rs#L84).

## Steps (TDD-first)

1. **Test first:** `kmeans` tests (2 clusters converge; deterministic seed; non-finite
   rejected) → implement k=2 Lloyd's on `[f32; D]`.
2. **Test first:** `ensemble` tests (consensus only when all models agree; 99th-pct
   boundary robust to one noisy training sample) → implement staggered models + consensus.
3. **Test first:** `window` tests (rate math; bit-pack/roll) → implement.
4. **Test first:** `sampler` tests with `FakeSampler` (deterministic) → implement
   `SysinfoSampler` (CPU needs two refreshes ≥200 ms apart; mem/disk/net; process top-N by
   **rank** with **executable basename** + an **optional cmdline hash**). **Full cmdline is not
   collected by default** — it is fetched only via the on-demand, audited, elevated path
   (CWE-214: argv routinely carries secrets, and redaction cannot know every app-specific
   format).
5. **Test first:** `redact` tests for known secret patterns (`--password=`, `token=`,
   `api[_-]?key=`, bearer, AWS keys, connection strings) → implement redaction; applied as
   **defense-in-depth on the on-demand full-cmdline path** (not a default-collection step).
6. Wire a bounded background task in `main.rs` (hard RAM/disk cap; training **yields** to
   session/control traffic; detection O(models), alloc-free post-load), default-off.

## Gotchas / constraints

- **No GPL** — clean-room from Netdata docs only. **No `unwrap()`** in production (use `?`);
  `#[non_exhaustive]` on public enums; `///` docs on public items.
- **Cardinality discipline:** process series are **top-N by rank** (rank is the series key);
  basename is a *value* carried for WS-3/WS-4, never a metric label. Full cmdline never on the
  default path.
- Redaction runs **at source** on the on-demand full-cmdline path before anything leaves the
  process boundary.
- **ARM footprint is a Wave 0 gate item:** benchmark CPU, RSS, allocations, and `sysinfo`
  process-enumeration cost on a small ARM target **before the WS-3 wire contract is frozen**;
  target «1% CPU, <1 MB RSS; keep default-off until it passes.

## Reviewer checklist

- [ ] Tests precede each module; positive + negative; redaction patterns covered.
- [ ] Clean-room (no GPL); no `unwrap()`; `#[non_exhaustive]`; docs on public items.
- [ ] Ring buffer + model count hard-capped; training yields; detection alloc-free.
- [ ] Process top-N by rank; default = basename (+ optional cmdline hash); no full cmdline by default.
- [ ] Default-off; agent never blocked by sentinel failure.

## Verification

`cd agent && cargo test -p mesh-agent-core` + clippy `-D warnings`; ARM bench recorded.
`/precommit` green. `/docs`: agent architecture page.
