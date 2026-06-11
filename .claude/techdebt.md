# Technical Debt Register

<!-- Ordered by severity. Update when debt is introduced or paid down. -->

## Severity: High

_None currently._

## Severity: Medium

### Cutover doc drift — Monitoring.md / Continuous-Deployment.md still describe the VPS path

The compose→OKE cutover (executed 2026-06-10) moved the app, monitoring, and CD
onto the OKE cluster and decommissioned the compose VM, but several canonical
docs still document the old VPS/compose access path:
[`docs/Monitoring.md`](../docs/Monitoring.md) (Grafana/Kuma/Loki access via
`ssh ubuntu@<VPS>` + `docker-compose.monitoring.yml`) and
[`docs/Continuous-Deployment.md`](../docs/Continuous-Deployment.md) (`/opt/opengate`
paths, `ssh ubuntu@<VPS>` rollback). Those commands no longer work — the VM is
gone. [`docs/Infrastructure.md`](../docs/Infrastructure.md)'s "Operator access"
section was updated as part of the decommission; the residual compose-deployment
references there describe the dormant rollback path and remain accurate.

The block-volume remediation ([ADR-035](../docs/adr/ADR-035-oke-free-tier-block-volume-remediation.md))
further changed the *cluster* monitoring topology the stale compose doc must be
rewritten against: `uptime-kuma` is **gone** (external uptime SaaS) — yet
[`docs/Monitoring.md`](../docs/Monitoring.md) still carries an Architecture diagram,
"Uptime Kuma" component/limit/retention rows, an access table, and an "Uptime Kuma
Monitors" section for it, plus `status.<domain>` references — and Grafana /
staging-Postgres / staging-`/data` now ride `emptyDir`. The narrowly-stale facts my
change actively *broke* (`make tunnel`, the Kubernetes.md backup row, Database.md
§Backups, the Home.md index line, the migration runbook's service count) were fixed
in place; the deferred sweep owns the full compose→OKE rewrite of Monitoring.md, not
those individual lines.

**Pay-down trigger:** a focused [`/wiki-audit`](../.claude/skills/wiki-audit/) pass
repointing the monitoring + CD docs at the cluster (`kubectl` access, `helm` /
`cd.yml` k8s deploy jobs, the ADR-035 monitoring topology) — out of scope for the
VM-decommission and block-volume changes.

### ADR-035 block-volume remediation — reclaim DONE; residual external follow-ups (Low)

The live-cluster reclaim ([ADR-035](../docs/adr/ADR-035-oke-free-tier-block-volume-remediation.md))
was executed 2026-06-11: block storage is now **3 × 50 GB + 50 GB node boot =
200 GB**, at the cap (was 450 GB), so the overage no longer bills. `helm upgrade`
of monitoring + prod + staging dropped the 4 helm-managed PVCs (kuma, grafana,
prod-backups, staging server-data); the staging postgres StatefulSet was
deleted-and-recreated on `emptyDir` (its `volumeClaimTemplates` are immutable) and
its orphaned PVC deleted by hand. Verified: `oci bv volume list` = 3 AVAILABLE,
prod `data-opengate-postgres-0` untouched + `/healthz` ok, Grafana healthy on
emptyDir, and a manual CronJob run landed `opengate-<ts>.sql.gz` in the
`opengate-pg-backups` bucket via the write-only PAR (retention = a 7-day Object
Storage lifecycle policy; a one-time IAM grant `opengate-os-lifecycle` lets the OS
service principal run it).

**Residual (no longer billing — Low):**
1. **External uptime SaaS** (user — needs an account): create UptimeRobot/Better
   Stack monitors on `https://opengate.cloudisland.net/healthz` (+ optional TCP on
   QUIC 9090 / MPS 4433), alert contact = the existing Telegram/email, enable the
   status page. Removing `uptime-kuma` left no in-cluster uptime probe until this
   exists (Grafana metric alerts still fire; `/healthz` still serves).
2. **Cloudflare DNS** (user): retire `status.opengate.cloudisland.net` or CNAME it
   to the SaaS status page.
3. **IaC drift (minor):** the backup bucket + PAR + lifecycle + the
   `opengate-os-lifecycle` IAM policy were created via the `oci` CLI (per the chart
   [`NOTES.txt`](../deploy/helm/opengate/templates/NOTES.txt)), not terraform.
   Codify the IAM policy + bucket in terraform if/when the backups infra is folded
   into IaC (the PAR stays a runtime credential, out of git).

### Phase 13b k8s — shared CA/VAPID/signing keys: mechanism shipped, runtime-unverified

**Largely paid down by PR-E (ADR-034).** The three per-replica keypairs the server
keeps under `/data` — enrollment CA (`ca.crt`/`ca.key`), VAPID (`vapid.json`), and
agent-update signing (`update-signing.json`) — can now be shared across replicas:
`server.sharedKeys.enabled` switches `/data` to an `emptyDir` and mounts the four
files read-only via `subPath` from `server.existingSecret` (the server loads keys
if present, so no code change), flipping the rollout to `RollingUpdate` and
dropping the per-replica PVC. The mechanism renders + validates under
`make lint-k8s` (enabled in `ci/test-values.yaml`).

**Residual:** the shared-keys path has only been **lint-validated**, never run on a
live multi-replica cluster — key loading from the secret mounts, rolling updates
with shared material, and cross-replica mTLS/push/update-verify are unproven at
runtime. **Pay-down trigger:** at the multi-replica cutover (same gate as the Redis
operational-surface and internal-listener-NetworkPolicy items), populate the
`existingSecret` with the four key files (runbook recipe in
`secrets.example.yaml`), install KEDA, and verify enrollment + push + update
verification across replicas. Until then staging/production keep
`sharedKeys.enabled=false` (single-replica PVC) and autoscaling off.

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

### Phase 13b PR-C C2 — internal relay listener has no NetworkPolicy

The cross-server proxy (ADR-033) opens a private listener (`:9091` by default) on
every server pod for proxied peer connections. It is never fronted by the public
router or ingress, and carries a required `X-OpenGate-Proxy` loop-guard header
plus an optional `OPENGATE_PROXY_SECRET`, but there is **no NetworkPolicy**
restricting who may reach it — on a flat cluster overlay any pod can dial it. The
shared secret is the only in-band control when enabled.

**Pay-down trigger:** at the multi-replica cutover (same gate as the Redis
operational-surface item above), add a NetworkPolicy that admits the internal
port only from sibling server pods (podSelector on the server label), and make
`OPENGATE_PROXY_SECRET` mandatory rather than optional in production overlays.
Until then the proxy path is inert (`redis.enabled=false`, owner is always self).

## Severity: Low

### Go mutation score is sensitive to gremlins' runner-derived per-mutant timeout

gremlins sets each mutant's timeout to `coverage-dry-run-elapsed × timeout-coefficient`. The dry-run elapsed is a single, runner-load-sensitive measurement, so a fast/partial coverage phase shrinks the per-mutant budget and the Postgres-backed packages (which re-pay container/migration setup, ~20-40s each) false-time-out. Timed-out mutants are dropped from gremlins' kill count, so the reported Go score collapses with no real change in test quality — observed 2026-06-03 (run 26870189012): 770→241 kills, 85.5%→76.0%, below the 85% alert floor, all from 590 false timeouts vs 7 the night before.

Mitigated by pinning `timeout-coefficient: 10` in [`server/.gremlins.yaml`](../server/.gremlins.yaml) (2× default headroom) + pinning the gremlins version + a 90-min job cap. This is a mitigation, not a guarantee: a sufficiently slow/partial coverage run can still tighten the budget. Two residual fragilities remain: (1) Go's true score (~85.5%) sits razor-thin above the 85% alert floor in [`scripts/mutation-summarize.sh`](../scripts/mutation-summarize.sh) — a few extra surviving/timed-out mutants re-trip the alert; (2) if recurrence persists, consider isolating the slow DB-backed packages or feeding gremlins a stable baseline duration rather than the live dry-run.

### Test-technique gaps — Go property libs, Rust fuzz targets, web property/fuzz

Property-based and fuzz testing exist only on the wire protocol, split by language:
Go fuzzing ([`server/internal/protocol/codec_fuzz_test.go`](../server/internal/protocol/codec_fuzz_test.go))
and Rust `proptest` ([`agent/crates/mesh-protocol/tests/property_test.rs`](../agent/crates/mesh-protocol/tests/property_test.rs)).
Three gaps remain:

1. **Go property-based testing** — no `pgregory.net/rapid`, `leanovate/gopter`, or
   `testing/quick`. Server invariants (converters, pagination math, APF/AMT
   parsers, relay framing) rely on table-driven tests + the single protocol fuzz
   target.
2. **Rust dedicated fuzzing** — no `cargo-fuzz`/libfuzzer fuzz targets
   (`libfuzzer-sys` appears only transitively via webrtc-rs benches per
   [`agent/deny.toml`](../agent/deny.toml)). The agent's decoders have `proptest`
   but no continuous fuzz corpus.
3. **Web property/fuzz** — no `fast-check` or fuzzing for the TS client
   (form validation, Zustand reducers, API-response handling).

**Pay-down trigger:** expand opportunistically with the next substantial
test/hardening commit — `rapid` property tests for the Go protocol/parser
surfaces, a `cargo-fuzz` target for `mesh-protocol` decode, and `fast-check` for
the web store/validation logic. Prioritize parsing/boundary surfaces (highest
defect density), where the existing fuzz/proptest already focus.
