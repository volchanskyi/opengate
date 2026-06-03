# Technical Debt Register

<!-- Ordered by severity. Update when debt is introduced or paid down. -->

## Severity: High

_None currently._

## Severity: Medium

### Phase 13b k8s — server CA + VAPID keys are per-replica (blocks multi-replica)

The Helm chart (`deploy/helm/opengate`, ADR-030) mounts the server's `-data-dir`
`/data` — which holds the self-signed enrollment CA and the VAPID push keypair —
on a per-replica `ReadWriteOnce` PVC. This is correct for the **single-replica**
PR-B (the keys survive restarts), but a second replica would generate its **own**
CA + VAPID keys, so agents enrolled against replica A would fail mTLS against
replica B, and push subscriptions would split.

**Pay-down trigger:** before scaling the server past one replica (PR-C cross-server
proxy / PR-E HPA). Promote the CA + VAPID material to a shared `Secret` mounted
read-only into every replica (generated once at install, e.g. by a pre-install
Job or out-of-band like `existingSecret`), and drop the `/data` PVC to an
`emptyDir`. Until then `server.replicas` must stay `1` and a `PodDisruptionBudget`
/ HPA must not raise it.

### ADR-024 WebRTC dispatch — 3 residual uncaught mutants in `handler.rs`

`cargo mutants -p mesh-agent-core` leaves three uncaught mutants in
`session/handler.rs::handle_control`, all match-arm deletions:

- `ControlMessage::FileUploadRequest` arm — **equivalent mutant**. `FileHandler::handle_upload` only logs ("not yet implemented"); deleting the arm falls through to the `_ => debug!` branch, which is observationally identical (no frame, no state change). Killing it would require giving upload an observable side effect (e.g. an ack frame) — a business-logic change, deferred until upload is actually implemented.
- `ControlMessage::SwitchToWebRTC` arm — **live-WebRTC-stack**. Distinguishing the arm from the `_` fallthrough needs `handle_offer` to produce an answer frame, which requires a valid browser SDP offer + ICE gathering (network).
- `ControlMessage::IceCandidate` arm — **live-WebRTC-stack**. The peer-present path needs a remote description set (only `handle_offer` does that); without it, both the arm and `_` produce no observable effect.

The two cheaply-killable mutants from the original 7-mutant gap were closed with tests (`switch.rs::handle_ack` body + the `SwitchAck` dispatch arm), and the `session/handlers/webrtc.rs` bodies were added to `agent/.cargo/mutants.toml` `exclude_globs` (same live-stack rationale as the long-standing `webrtc.rs` exclusion — ADR-024 §9 merely relocated that code). These three remain because the `mutants.toml` glob mechanism cannot exclude individual match arms within an otherwise well-covered function. **Pay-down trigger:** revisit when file upload is implemented (closes the equivalent mutant) or when a headless WebRTC offer/answer harness exists (closes the two live-stack arms).

### Phase 13b PR-C — Redis Sentinel operational surface + dormant-untested HA

PR-C C1 (ADR-031) introduces a Redis Sentinel cluster as the backing store for
the distributed `SessionRegistry`. Two debts ride in with it:

1. **New operational surface.** Redis adds persistence (AOF+RDB), Sentinel quorum
   health, connection pooling, and backup/restore — none of which the in-process
   adapter needed. There is no Redis backup CronJob (the Postgres one is the
   model), no pool-size tuning, and no Redis monitoring/alerting yet.
2. **Dormant HA is lint-validated but not runtime-tested.** The Sentinel topology
   in [`deploy/helm/opengate`](../deploy/helm/opengate) is gated `redis.enabled`
   (default false) and renders clean under `make lint-k8s`, but master rediscovery
   on pod restart, automatic failover, and replica re-pointing have never run
   against a live cluster. The adapter's own logic is covered by miniredis unit
   tests; the gap is the **chart's** runtime behaviour.

**Pay-down trigger:** before flipping any environment overlay to
`REGISTRY_BACKEND=redis` (the multi-replica cutover, gated behind PR-C's C2
cross-server proxy + C3 readiness/degraded-mode). At that point: add a real-Redis
testcontainers integration test (mirrors [`testpg`](../server/internal/testpg/testpg.go)),
a Redis backup CronJob, Redis monitoring, and a kill-the-master failover drill in
a staging cluster. Until then the backend stays `inprocess` in every overlay.

## Severity: Low

### Go mutation score is sensitive to gremlins' runner-derived per-mutant timeout

gremlins sets each mutant's timeout to `coverage-dry-run-elapsed × timeout-coefficient`. The dry-run elapsed is a single, runner-load-sensitive measurement, so a fast/partial coverage phase shrinks the per-mutant budget and the Postgres-backed packages (which re-pay container/migration setup, ~20-40s each) false-time-out. Timed-out mutants are dropped from gremlins' kill count, so the reported Go score collapses with no real change in test quality — observed 2026-06-03 (run 26870189012): 770→241 kills, 85.5%→76.0%, below the 85% alert floor, all from 590 false timeouts vs 7 the night before.

Mitigated by pinning `timeout-coefficient: 10` in [`server/.gremlins.yaml`](../server/.gremlins.yaml) (2× default headroom) + pinning the gremlins version + a 90-min job cap. This is a mitigation, not a guarantee: a sufficiently slow/partial coverage run can still tighten the budget. Two residual fragilities remain: (1) Go's true score (~85.5%) sits razor-thin above the 85% alert floor in [`scripts/mutation-summarize.sh`](../scripts/mutation-summarize.sh) — a few extra surviving/timed-out mutants re-trip the alert; (2) if recurrence persists, consider isolating the slow DB-backed packages or feeding gremlins a stable baseline duration rather than the live dry-run.
