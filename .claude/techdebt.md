# Technical Debt Register

<!-- Ordered by severity. Update when debt is introduced or paid down. -->
<!-- Last reviewed: 2026-06-19; reconnect-backoff flap-guard shipped (ReconnectGovernor + full-jitter, entry cleared); agent ticket cache (W3) remains. -->

## Severity: High

_None currently._

## Severity: Medium

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
change like W1. W1's coordinated cutover has since shipped (production agent on
0.45.0, client-first) **without** resumption, so this is now a standalone
follow-up rather than part of that cutover. **Pay-down trigger:** quinn caches and
presents tickets and a reconnecting production agent is observed resuming
(`DidResume`).

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

## Severity: Low

### ADR-024 WebRTC dispatch — 1 residual equivalent mutant in `handler.rs`

`cargo mutants -p mesh-agent-core` leaves one uncaught mutant in `session/handler.rs::handle_control`: the `ControlMessage::FileUploadRequest` match-arm deletion. It is an **equivalent mutant** — `FileHandler::handle_upload` only logs ("not yet implemented"), so deleting the arm is observationally identical to the `_ => debug!` fallthrough (no frame, no state change). Killing it requires giving upload an observable side effect (e.g. an ack frame), a business-logic change deferred until upload is implemented.

The two former live-WebRTC-stack arms (`SwitchToWebRTC`, `IceCandidate`) are now **caught**: both route through a `WebRtcDispatch` trait seam whose production delegate (`RealWebRtcDispatch`) lives in the already-excluded `session/handlers/webrtc.rs`, while a recording mock in `session/handler.rs` tests asserts that — and with what arguments — each arm dispatches. This kills the arm-deletion mutants without a live media stack and adds no new surviving mutants (the delegate sits in the excluded file). The earlier `SwitchAck` arm + `switch.rs::handle_ack` mutants were closed previously.

**Pay-down trigger:** revisit when file upload is implemented (closes the last equivalent mutant).

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

### Go mutation run — sharded to fix cap-cancellation + timeout fragility (confirmation pending)

gremlins sets each mutant's timeout to `coverage-dry-run-elapsed × timeout-coefficient`, a single runner-load-sensitive measurement: a fast/partial coverage phase shrinks the per-mutant budget and the Postgres-backed packages (which re-pay container/migration setup, ~20-40s each) false-time-out, dropping kills and collapsing the reported score with no real test-quality change (2026-06-03 run 26870189012: 770→241 kills, 85.5%→76.0%, below the 85% floor). Separately, the monolithic `gremlins unleash .` grew past the 100-min job cap (cancelled 2026-06-18/19) while the mutant count stayed flat, so no report reached the trend pipeline.

Resolved by sharding the Go leg horizontally — [`scripts/lib/mutation-shards.sh`](../scripts/lib/mutation-shards.sh) is the single source of truth (partition asserted by [`scripts/tests/mutation-workflow.test.sh`](../scripts/tests/mutation-workflow.test.sh), mirrored by `make mutate-go`). Each shard runs the whole module for coverage but restricts *mutation* to its packages via `--exclude-files`, balanced by CI cost (DB packages dominate; `api` is isolated on its own shard). `GOFLAGS=-count=1` forces a real coverage dry-run so the budget no longer collapses to the restored test cache. The 100-min monolith cap became a per-shard 75-min cap with headroom; per-shard reports are merged by [`scripts/mutation-merge-go.sh`](../scripts/mutation-merge-go.sh) into the canonical report (a missing shard fails the merge → reported as an incomplete run, never a partial score).

**Pay-down trigger:** confirm across 3 consecutive nightly runs that no shard is cancelled and that `mutation_score{language="go"}` reaches VictoriaMetrics — the trend push has not yet completed for a full mutation run. `timeout-coefficient: 15` in [`server/.gremlins.yaml`](../server/.gremlins.yaml) could then drop for a pure shard via a per-shard `--config` override.

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
