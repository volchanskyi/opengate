# Technical Debt Register

<!-- Ordered by severity. Track only ACTIVE debt: when an item's pay-down trigger is met, delete it (the git history + the relevant ADR are the record). Do not keep resolved items or historical narrative here. -->
<!-- Last reviewed: 2026-06-29. -->

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
(falls back to a full handshake when no ticket is cached). **Pay-down trigger:**
quinn caches and presents tickets and a reconnecting production agent is observed
resuming (`DidResume`).

## Severity: Low

### ADR-035 — residual external uptime/DNS follow-ups (user-owned)

The OKE free-tier block-volume remediation
([ADR-035](../docs/adr/ADR-035-oke-free-tier-block-volume-remediation.md)) is
complete; only two **external, user-owned** follow-ups remain (neither bills):

1. **External uptime SaaS** (user — needs an account): create UptimeRobot/Better
   Stack monitors on `https://opengate.cloudisland.net/healthz` (+ optional TCP on
   QUIC 9090 / MPS 4433), alert contact = the existing Telegram/email, enable the
   status page. Removing `uptime-kuma` left no in-cluster uptime probe until this
   exists (Grafana metric alerts still fire; `/healthz` still serves).
2. **Cloudflare DNS** (user): retire `status.opengate.cloudisland.net` or CNAME it
   to the SaaS status page.

### ADR-024 WebRTC dispatch — 1 residual equivalent mutant in `handler.rs`

`cargo mutants -p mesh-agent-core` leaves one uncaught mutant in
`session/handler.rs::handle_control`: the `ControlMessage::FileUploadRequest`
match-arm deletion. It is an **equivalent mutant** — `FileHandler::handle_upload`
only logs ("not yet implemented"), so deleting the arm is observationally
identical to the `_ => debug!` fallthrough (no frame, no state change). Killing it
requires giving upload an observable side effect (e.g. an ack frame), a
business-logic change deferred until upload is implemented.

**Pay-down trigger:** revisit when file upload is implemented (closes the last equivalent mutant).

### `web/package.json` TypeScript pinned to ^5.9.3 — `openapi-typescript` peer conflict

TypeScript is pinned to `^5.9.3` because `openapi-typescript@7.13.0` (used by
`npm run generate:api`) declares `peerDependencies: { typescript: "^5.x" }`. A
lenient `npm install` resolves past the conflict, but a clean `npm ci` (the
`build-image.yml` Docker build, `node:24-alpine`) fails hard with `ERESOLVE` on
TypeScript 6.x.

**Pay-down trigger:** revisit once `openapi-typescript` ships a release supporting
TypeScript 6.x (`npm view openapi-typescript versions` / its peerDependencies
range), then bump both together.

### Go mutation run — sharded; nightly confirmation pending

The Go mutation leg is sharded horizontally to fix prior cap-cancellation +
timeout-budget fragility — [`scripts/lib/mutation-shards.sh`](../scripts/lib/mutation-shards.sh)
is the single source of truth (partition asserted by
[`scripts/tests/mutation-workflow.test.sh`](../scripts/tests/mutation-workflow.test.sh),
mirrored by `make mutate-go`). Each shard mutates only its packages
(`--exclude-files`), `GOFLAGS=-count=1` forces a real coverage dry-run so the
per-mutant budget can't collapse onto the restored test cache, and per-shard
reports merge via [`scripts/mutation-merge-go.sh`](../scripts/mutation-merge-go.sh)
(a missing shard fails the merge rather than reporting a partial score).

**Pay-down trigger:** confirm across 3 consecutive nightly runs that no shard is
cancelled and that `mutation_score{language="go"}` reaches VictoriaMetrics; then
`timeout-coefficient: 15` in [`server/.gremlins.yaml`](../server/.gremlins.yaml)
could drop for a pure shard via a per-shard `--config` override.

### Test-technique gaps — RESOLVED

Property/fuzz testing now spans all three languages. Go: the protocol fuzz
[`codec_fuzz_test.go`](../server/internal/protocol/codec_fuzz_test.go) plus
`pgregory.net/rapid` property tests over the APF parsers, model→API converters,
pagination math, and relay framing. Rust: `proptest`
([`property_test.rs`](../agent/crates/mesh-protocol/tests/property_test.rs)) plus a
`cargo-fuzz` libFuzzer target over `Frame::decode`
([`agent/fuzz/fuzz_targets/decode.rs`](../agent/fuzz/fuzz_targets/decode.rs)),
gated to a bounded nightly job with the stable corpus replay
([`decode_corpus_test.rs`](../agent/crates/mesh-protocol/tests/decode_corpus_test.rs))
as the always-run guard. Web: `fast-check` property suites over the highest-value
TS surfaces — token-status validation
([`token-status.property.test.ts`](../web/src/lib/token-status.property.test.ts)),
the `file-store` reducer over arbitrary action sequences
([`file-store.property.test.ts`](../web/src/features/file-manager/state/file-store.property.test.ts)),
and the wire codec parser/roundtrip
([`codec.property.test.ts`](../web/src/lib/protocol/codec.property.test.ts)). The
web suite surfaced a fail-open defect (an unparseable token expiry was treated as
not-expired) which was fixed in [`token-status.ts`](../web/src/lib/token-status.ts).
All suites run deterministically under vitest (pinned `numRuns` + `seed`).
