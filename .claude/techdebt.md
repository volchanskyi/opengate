# Technical Debt Register

<!-- Ordered by severity. Update when debt is introduced or paid down. -->

## Severity: High

_None currently._

## Severity: Medium

### W1 client-first handshake — breaking wire change needs a coordinated agent/server rollout

[`handshaker.go`](../server/internal/agentapi/handshaker.go) now reads `0x11`
AgentHello before writing `0x10` ServerHello, the agent
([`main.rs`](../agent/crates/mesh-agent/src/main.rs)) now `open_bi`s and writes
first, and the server ([`server.go`](../server/internal/agentapi/server.go))
`AcceptStream`s instead of opening. This is a breaking wire-protocol change: a
client-first server cannot complete a handshake with a server-first agent, and
vice versa.

Per the master plan's §3 rollout decision, **option (a) — coordinated
cutover** — is the call here: production has exactly one agent (verified
2026-06-11) on an Ed25519-signed QUIC auto-update channel (Phase 14), so a
dual-mode (peek-first-frame) handshake would add complexity for a fleet of
one. The next production deploy of the server **must** ship together with (or
immediately followed by) an agent auto-update push — deploying the new server
alone will strand the running agent mid-reconnect until it updates.

**Pay-down trigger:** clear this note once the coordinated deploy has shipped
and the production agent is confirmed connected post-cutover. W2 (`0x14` fast
path), W3 (0-RTT), W4 (ADR), and W5 (remove workaround-era comments + bounded
accept timeout) remain open on the active fast-path-reconnect-fix master plan
under `.claude/plans/`.

### Cutover doc drift — Monitoring.md still describes the compose stack

The compose→OKE cutover moved the app, monitoring, and CD onto the OKE cluster,
but [`docs/Monitoring.md`](../docs/Monitoring.md) still documents the old
compose access and deployment path. [`docs/Continuous-Deployment.md`](../docs/Continuous-Deployment.md)
now describes the Kubernetes-only workflow. Residual compose references in
[`docs/Infrastructure.md`](../docs/Infrastructure.md) describe the dormant
rollback artifacts and remain accurate.

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
repointing the monitoring docs at the cluster and the ADR-035 monitoring
topology — out of scope for the
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

### ADR-024 WebRTC dispatch — 3 residual uncaught mutants in `handler.rs`

`cargo mutants -p mesh-agent-core` leaves three uncaught mutants in
`session/handler.rs::handle_control`, all match-arm deletions:

- `ControlMessage::FileUploadRequest` arm — **equivalent mutant**. `FileHandler::handle_upload` only logs ("not yet implemented"); deleting the arm falls through to the `_ => debug!` branch, which is observationally identical (no frame, no state change). Killing it would require giving upload an observable side effect (e.g. an ack frame) — a business-logic change, deferred until upload is actually implemented.
- `ControlMessage::SwitchToWebRTC` arm — **live-WebRTC-stack**. Distinguishing the arm from the `_` fallthrough needs `handle_offer` to produce an answer frame, which requires a valid browser SDP offer + ICE gathering (network).
- `ControlMessage::IceCandidate` arm — **live-WebRTC-stack**. The peer-present path needs a remote description set (only `handle_offer` does that); without it, both the arm and `_` produce no observable effect.

The two cheaply-killable mutants from the original 7-mutant gap were closed with tests (`switch.rs::handle_ack` body + the `SwitchAck` dispatch arm), and the `session/handlers/webrtc.rs` bodies were added to `agent/.cargo/mutants.toml` `exclude_globs` (same live-stack rationale as the long-standing `webrtc.rs` exclusion — ADR-024 §9 merely relocated that code). These three remain because the `mutants.toml` glob mechanism cannot exclude individual match arms within an otherwise well-covered function. **Pay-down trigger:** revisit when file upload is implemented (closes the equivalent mutant) or when a headless WebRTC offer/answer harness exists (closes the two live-stack arms).

## Severity: Low

### `web/package.json` TypeScript pinned to ^5.9.3 — `openapi-typescript` peer conflict

While applying Dependabot's npm-deps group bump (17 packages), TypeScript
6.0.3 was reverted back to `^5.9.3`. `openapi-typescript@7.13.0` (latest
available; used by `npm run generate:api` for the OpenAPI-driven TS client
codegen) declares `peerDependencies: { typescript: "^5.x" }`. A lenient
`npm install` resolved past the conflict locally, but a clean `npm ci` (as run
in the `build-image.yml` Docker build, `node:24-alpine`, npm 11.13) fails hard
with `ERESOLVE` — confirmed by reproducing the exact error. All other bumps
from that PR (react/react-dom/react-router-dom/zustand/playwright/vite/eslint/
typescript-eslint/etc.) landed; only the TypeScript major stayed back.

**Pay-down trigger:** revisit once `openapi-typescript` ships a release
supporting TypeScript 6.x (`npm view openapi-typescript versions` / its
peerDependencies range), then bump both together.

### Docker Hub authenticated fallback awaits workflow verification

The shared
[`docker-hub-mirror` action](../.github/actions/docker-hub-mirror/action.yml)
now supports authenticated direct Docker Hub fallback, and every protected
workflow passes the optional repository credentials. The repository has both
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`; the login intentionally skips when
credentials are unavailable so forked pull requests remain runnable.

**Pay-down trigger:** confirm a protected workflow logs the successful
authenticated-login message without exposing either value.

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

### Performance benchmarks — no CI regression detection

Go and Rust micro-benchmarks run in the precommit gauntlet (`go test -bench` /
`cargo bench -p mesh-protocol`) but only assert they execute — **no perf
thresholds or cross-commit regression tracking** are enforced, and there is no CI
benchmark workflow.

**Pay-down trigger:** wire benchmark deltas into CI with a regression alert.
Working plan: [`performance-benchmarks.md`](plans/archive/performance-benchmarks.md).
