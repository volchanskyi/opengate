# Technical Debt Register

<!-- Ordered by severity. Track only ACTIVE debt: when an item's pay-down trigger is met, delete it (the git history + the relevant ADR are the record). Do not keep resolved items or historical narrative here. -->
<!-- Last reviewed: 2026-07-26; terminal/session lifecycle correction added no debt. -->

## Severity: High

_None currently._

## Severity: Medium

### Multi-org membership API and web org switcher deferred

WS-0 satisfies "web carries org context" by retaining the JWT `org` claim in the
auth store as display/UX state only; the server derives authorization scope from
the signed token and never trusts a browser-supplied org value. There is not yet a
multi-org membership API, so a web org switcher has no authoritative membership
surface to switch between. The deferred multi-org design must also settle:

1. Split platform-admin from org-scoped admin. Today `users.is_admin` is mirrored
   from Administrators membership and drives the `app.is_admin` RLS policy bypass;
   that is correct only while every user is in the default org.
2. Decide whether `organizations` itself remains globally enumerable or gains a
   membership-scoped read surface once users can belong to more than one org.
3. Decide the login/email uniqueness model. The current global `users.email`
   uniqueness keeps login lookup unambiguous, but it also blocks per-org email
   reuse and makes the new `(org_id, email)` index advisory until multi-org
   membership exists.
4. Reconcile globally unique `security_groups.name` with per-org system groups
   before creating non-default organizations that need their own Administrators
   group name.

**Pay-down trigger:** when multi-org membership is introduced, add the server
membership/switching API, issue refreshed tokens for the selected org, split
platform-admin from org-admin bypass semantics, choose the org/email/security
group uniqueness model, decide the organization visibility model, and build the
web org switcher against that server-trusted flow.

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

### Per-test migration replay is real cost but not on the wall-clock critical path

[`NewTestStore`](../server/internal/testutil/testutil.go) creates a schema and
applies every migration for each of its 56 call sites. Measured against a warm
shared Postgres: **~356 ms** per store for schema + migrations, versus **~80 ms**
for a `CREATE DATABASE … TEMPLATE` clone of a pre-migrated template — a 4.4x
per-operation gain. A full template-clone implementation (content-addressed
template, advisory-lock creation protocol, scratch-and-rename publish) was built
and measured end-to-end: `internal/api` came out at **33.29 s** cloned versus
**32.84 s** on the existing schema path, i.e. no improvement. Store creation is
not the critical path under test parallelism, and `CREATE DATABASE` serializes on
the shared template, which cancels the per-operation win. The implementation was
therefore dropped rather than landed; the isolation contract it exercised is kept
in [`testutil_test.go`](../server/internal/testutil/testutil_test.go).

**Pay-down trigger:** store creation becomes the measured bottleneck — e.g. if
`maxLiveStores` is raised, or a package's runtime is shown to be dominated by
migration time rather than by the tests themselves.

### E2E worker-scoped identities not adopted; Playwright stays single-worker

46 of 56 Playwright tests provision their own account
([`fixtures.ts`](../web/e2e/fixtures.ts)), and `workers: 1` is a deliberate fix
for `createAdminUser` racing on shared IAM state. Sharing one identity per worker
would cut account setup and is the prerequisite for raising the worker count, but
it makes every test that writes devices, groups, or users visible to its
siblings. Adopting it needs a per-test audit classifying which of the 19 spec
files mutate state that another test observes; guessing at that trades a known
cost for unpredictable cross-test flake. The per-test navigation cost has been
halved in the meantime (the token is seeded via `addInitScript`, so an
authenticated page is reached in one navigation instead of two).

Two pieces of that audit are now enforced rather than pending. The
`globalTeardown` in [`global-teardown.ts`](../web/e2e/global-teardown.ts) fails
any run that leaves a group behind, and
[`fleet-stub.ts`](../web/e2e/helpers/fleet-stub.ts) gives the specs that assert
an empty fleet a way to supply that emptiness instead of reading shared state.
Both narrow the audit to specs that seed devices or users, and both hold at any
worker count. Since the organization — not the creating user — is the visibility
boundary, per-worker identities would not isolate fleet writes on their own; the
remaining audit has to cover that, or the worker count needs a per-worker
organization.

**Pay-down trigger:** E2E wall time becomes a merge-latency problem, making the
per-test mutation audit worth its cost.

### Device-list filtering is client-side (future server-side concern at scale)

The Dashboard/Fleet-Health deep links narrow the device list with a pure
client-side reducer ([`device-filter.ts`](../web/src/features/devices/device-filter.ts)):
the full device list is already fetched for the grid, so `status`/`maintenance`/
`health` filtering happens in the browser with no extra round-trip. This is
correct and cheap at the current fleet size (the list endpoint has no server-side
pagination), but at **>20k agents** the unpaginated list fetch itself becomes the
bottleneck, and filtering should move behind a paginated, server-filtered
`/devices` query (matching the multiscale-readiness scaling posture). No action
needed until list-fetch latency shows up in practice.

**Pay-down trigger:** device-list fetch latency or payload size becomes a
problem as the fleet approaches the >20k-agent scaling tier.

### On-demand network drills deferred

The deployed fault drills are active in staging CD: with the `STAGING_FAULT_TESTS`
repository variable set to `true`,
[`cd.yml`](../.github/workflows/cd.yml) runs a
[`fault-tolerance.yml`](../.github/workflows/fault-tolerance.yml) drill against
`opengate-staging` after E2E and gates production promotion on its result. The
runner surface covers `pod-delete`, `bad-rollout`, `ingress-504`, and
`ingress-502` (`STAGING_FAULT_PROFILE` selects one; default `pod-delete`), and
the node scrape (`up`, node-exporter, `/metrics`, ingress logs) is live in
VictoriaMetrics so infra scenarios have usable CPU/mem/disk evidence.

On-demand network drills stay deferred: packet loss/corrupt/partition on the QUIC
path (a privileged CRI-O daemon for one pod) is disproportionate today and is
never wired into the gating path. Build the network-drill tooling only when a
storm/lossy-network scenario needs it (see
[Fault-Injection](../docs/Fault-Injection.md)).

**Pay-down trigger:** the network-drill item closes only if/when a lossy-network
scenario is actually needed.

### Edge-Sentinel audited command-line redaction not wired into sampler output

`redact_cmdline` is implemented and tested in the agent ML redaction module, but
the live sampler currently stores only a process basename plus optional
`cmdline_hash`; it does not emit redacted command lines. That is intentional for
WS-2's default-off local sampler, but the audited on-demand flow must wire the
redactor before any raw command-line text leaves the agent.

**Pay-down trigger:** when an audited command-line collection/reporting path is
added, route command lines through `redact_cmdline` before serialization and add
an end-to-end test that proves secrets are redacted in the emitted payload.

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

### Mutation workflow — recovered sharding; nightly confirmation pending

Rust and Go are sharded horizontally to restore headroom under the existing job
cap. Rust uses round-robin distribution so expensive source clusters do not
collect in one consecutive slice, and the agent API is divided into file units.
The timeout-heavy backfill and handshake files run independently, and shard ids
describe either the Rust selector or the Go behavior they own.
[`scripts/lib/mutation-shards.sh`](../scripts/lib/mutation-shards.sh) is the
single source of truth for expected shards and Go file/directory mutation units;
[`scripts/tests/mutation-workflow.test.sh`](../scripts/tests/mutation-workflow.test.sh)
proves every non-test Go source is assigned once or explicitly excluded. Go keeps
module-wide coverage with `GOFLAGS=-count=1`, while strict Rust/Go merges and
[`scripts/mutation-status-build.sh`](../scripts/mutation-status-build.sh) prevent
an incomplete artifact set from becoming a canonical score row. Every run still
publishes run/shard completion status for diagnosis. `go-agentapi-backfill`
carries a scoped-down gremlins timeout coefficient
(`mutation_go_shard_timeout_coefficient` in
[`scripts/lib/mutation-shards.sh`](../scripts/lib/mutation-shards.sh), consumed by
both the workflow and `make mutate-go`): its `conn_backfill.go` guard-clause
mutants block under the Postgres harness and TIME OUT, which already counts as
caught, so the tighter budget ends those timeout waves early and keeps headroom
under the 75-minute cap without changing the score. Every other shard inherits
the baseline in `server/.gremlins.yaml`.

**Pay-down trigger:** after score repair clears the existing floor, confirm three
consecutive scheduled runs with every shard complete, at least ten minutes of
per-shard headroom, and Rust/Go/Web score plus completion series present in
VictoriaMetrics. Only then close the recovery plan.
