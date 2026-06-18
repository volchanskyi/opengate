# Technical Debt Register

<!-- Ordered by severity. Update when debt is introduced or paid down. -->
<!-- Last reviewed: 2026-06-17; DD-E docs-as-code introduced no new debt. -->

## Severity: High

_None currently._

## Severity: Medium

### W1+W2 client-first handshake + 0x14 fast path — breaking wire change needs a coordinated agent/server rollout

[`handshaker.go`](../server/internal/agentapi/handshaker.go) reads the agent's
first message and branches: `0x11` AgentHello → full handshake (reply
ServerHello); `0x14` SkipAuth → fast-path reconnect (verify the cached CA hash
is current, skip the reply). The agent
([`main.rs`](../agent/crates/mesh-agent/src/main.rs)) `open_bi`s and writes
first — a full handshake on cold start (caching the CA hash from ServerHello),
then `0x14` on reconnect with full-handshake fallback on rejection — and the
server ([`server.go`](../server/internal/agentapi/server.go)) `AcceptStream`s
instead of opening. This is a breaking wire-protocol change: a client-first
server cannot complete a handshake with a server-first agent, and vice versa.

Per the master plan's §3 rollout decision, **option (a) — coordinated
cutover** — is the call here: production has exactly one agent (verified
2026-06-11) on an Ed25519-signed QUIC auto-update channel (Phase 14), so a
dual-mode (peek-first-frame) handshake would add complexity for a fleet of
one. The next production deploy of the server **must** ship together with (or
immediately followed by) an agent auto-update push — deploying the new server
alone will strand the running agent mid-reconnect until it updates.

**Settled auth model (input for the W4 ADR):** authentication is **mTLS-only** —
the QUIC/TLS layer (`RequireAndVerifyClientCert`) authenticates the agent and
the agent verifies the server against its CA; the message exchange only binds
the cert hash and advertises the CA hash. There is **no app-layer signature
exchange** (`0x12`/`0x13` remain unused constants). So `0x14` changes
**round-trips, not cryptographic cost**: it elides the `0x10`/`0x11` exchange,
not signatures. The dominant per-reconnect cost is the **TLS mTLS handshake
itself, which `0x14` does not avoid** — only 0-RTT/session resumption (W3)
does. **Reviewer call (recorded):** ship W2 now — the saved round-trip helps
reconnection-storm latency on its own, and W3 stacks the larger TLS-cost win on
top.

**Pay-down trigger:** clear this note once the coordinated deploy has shipped
and the production agent is confirmed connected post-cutover. W3's evaluation is
**done** (decision recorded below); W4 (ADR — capture the settled mTLS-only model
above plus W3's resumption decision) and W5 (remove workaround-era comments +
bounded accept timeout) remain open on the active fast-path-reconnect-fix master
plan under `.claude/plans/`.

### W3 decision — adopt 1-RTT TLS session resumption; agent-side enablement pending; 0-RTT deferred

The W3 spike ([`quic_resumption_test.go`](../server/internal/agentapi/quic_resumption_test.go)
+ paired benchmarks) settled the storm-cost question empirically against quic-go
v0.60.0 and the repo's mTLS config: **1-RTT TLS session resumption** completes
under `RequireAndVerifyClientCert`, **preserves the verified client identity**
server-side (`DidResume==true`, `PeerCertificates` retained), and cuts the
per-reconnect cost ~23% / ~360µs (~207 fewer allocs) by skipping the asymmetric
handshake. **Decision: adopt 1-RTT resumption, defer 0-RTT** — 0-RTT works with
mTLS on this version but its early data is replayable and it saves only latency,
not crypto, on top of resumption (full replay analysis in the archived
[W3 plan](plans/archive/fast-path-w3-0rtt-eval.md)).

**Server: no change.** Go/quic-go issues session tickets by default and the spike
confirms resumption against the unmodified `ServerTLSConfig` with `Allow0RTT` off
(kept off to foreclose 0-RTT replay). `TestQUICSessionResumption_PreservesMTLSIdentity`
is the always-run regression guard.

**Residual (the debt):** the quinn agent
([`main.rs`](../agent/crates/mesh-agent/src/main.rs)) does not yet enable TLS
session resumption or persist a session-ticket cache across reconnects, so the
production saving is not realized. It is a backward-compatible client-side change
(falls back to a full handshake when no ticket is cached) — not a breaking wire
change like W1 — but it should ship with an agent reconnect verification inside
W1's coordinated cutover. **Pay-down trigger:** quinn caches and presents tickets
and a reconnecting production agent is observed resuming (`DidResume`).

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
shipped 2026-06-11 — block storage is back at the **200 GB** free-tier cap and no
longer bills (full execution record lives in the ADR). Only external,
no-longer-billing follow-ups remain:

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
`cargo bench -p mesh-protocol`) and CI now publishes their results (the
Go/Rust Benchmark + Publish Benchmarks + Publish Performance Data jobs in
[`ci.yml`](../.github/workflows/ci.yml)). They still only assert the benchmarks
execute and record numbers — **no perf thresholds or cross-commit regression
tracking** gate a commit.

**Pay-down trigger:** wire benchmark deltas into CI with a regression alert.
Working plan: [`performance-benchmarks.md`](plans/archive/performance-benchmarks.md).
