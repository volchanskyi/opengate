# Technical Debt Register

<!-- Ordered by severity. Track only ACTIVE debt: when an item's pay-down trigger is met, delete it (the git history + the relevant ADR are the record). Do not keep resolved items or historical narrative here. -->
<!-- Last reviewed: 2026-08-17; the investigations workspace left the web bundle within 10 KB of its budget. -->

## Severity: High

_None currently._

## Severity: Medium

### The QUIC accept path has no in-process test harness

[`server.go`](../server/internal/agentapi/server.go) and
[`server_connection.go`](../server/internal/agentapi/server_connection.go) are the
only production files left in `sonar.coverage.exclusions`. They sit at ~4–80% line
coverage because reaching `accept` means standing up a real QUIC listener, driving
a TLS handshake with a client certificate, and holding a peer connection open —
none of which any current test does. The package's QUIC tests exercise resumption
and tombstones through other entry points, so the accept, register, teardown and
control-loop paths run only in production and in e2e.

That is a genuine gap, not a classification: an in-process harness *is* buildable
(a `quic.Transport` on a loopback UDP socket with a self-signed cert pair), which
is exactly why the exclusion is debt rather than a permanent carve-out. Until it
exists, a change to the connection lifecycle is defended by e2e alone.

**Pay-down trigger:** the next change to the connection lifecycle, or any further
split of these files. Build the loopback harness, cover accept → register →
control loop → unregister, then delete both entries and their justification lines
from [`sonar-project.properties`](../sonar-project.properties) — the guard
([`sonar-coverage-exclusion-guard.sh`](../scripts/sonar-coverage-exclusion-guard.sh))
will then hold the list at zero production exclusions.

### Multi-tenant membership API and web tenant switcher deferred

WS-0 satisfies "web carries tenant context" by retaining the JWT `tenant` claim in the
auth store as display/UX state only; the server derives authorization scope from
the signed token and never trusts a browser-supplied tenant value. There is not yet a
multi-tenant membership API, so a web tenant switcher has no authoritative membership
surface to switch between. The deferred multi-tenant design must also settle:

1. Split platform-admin from tenant-scoped admin. Today `users.is_admin` is mirrored
   from Administrators membership and drives the `app.is_admin` RLS policy bypass;
   that is correct only while every user is in the default tenant.
2. Decide whether `tenants` itself remains globally enumerable or gains a
   membership-scoped read surface once users can belong to more than one tenant.
3. Decide the login/email uniqueness model. The current global `users.email`
   uniqueness keeps login lookup unambiguous, but it also blocks per-tenant email
   reuse and makes the new `(tenant_id, email)` index advisory until multi-tenant
   membership exists.
4. Reconcile globally unique `security_groups.name` with per-tenant system groups
   before creating non-default tenants that need their own Administrators
   group name.

**Pay-down trigger:** when multi-tenant membership is introduced, add the server
membership/switching API, issue refreshed tokens for the selected tenant, split
platform-admin from tenant-admin bypass semantics, choose the tenant/email/security
group uniqueness model, decide the tenant visibility model, and build the
web tenant switcher against that server-trusted flow.

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

### Device delete without VictoriaMetrics skips the erasure guarantees

[`purgeDeletedDevice`](../server/internal/api/handlers_device_actions.go) falls back
to a plain `devices.Delete` when no purge orchestrator is wired, which is what
happens when `--victoriametrics-url` is unset. That path records no tombstone and
no purge job, and it does not run
[`EraseDeviceAlerts`](../server/internal/alerts/postgres.go), so the incident
counts a foreign key cannot repair are left describing a machine that is gone —
an operator reads "40 machines" on a room whose fortieth was deleted. Alerts and
incidents work in that configuration (they need Postgres, not VictoriaMetrics), so
the gap is reachable rather than theoretical; every deployed environment wires
VictoriaMetrics, which is why this is Low. **Pay-down trigger:** the fallback is
either given the same erasure path as the orchestrator, or removed so a delete
without a purger is refused outright.

### OpenAPI request constraints are documentation, not runtime validation

[`api/openapi.yaml`](../api/openapi.yaml) carries `maxLength` on some request
fields, but no request-validating middleware runs: `kin-openapi` is pulled in
only so [`openapi_gen.go`](../server/internal/api/openapi_gen.go) can embed the
spec, and the generated strict handler enforces types and required-ness, not
string bounds. Every schema constraint is therefore advisory, and a field is
only bounded where a handler says so.

Free-text request fields are bounded in
[`validate.go`](../server/internal/api/validate.go) instead: `invalidText`
rejects with a 400 where the contract has one (register, create-group) and
`sanitizeText` strips control characters and truncates where it does not
(update-user display name, restart/maintenance reason, enrollment label). Both
bound by rune count.

**Pay-down trigger:** if a future endpoint needs a constraint the handler cannot
express, mount `OapiRequestValidator` from `oapi-codegen/nethttp-middleware` on
the API group and let the spec become enforcing — then add the missing `400`
responses to the restart, maintenance, and enrollment operations so those
handlers can reject rather than sanitize.

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
worker count. Since the tenant — not the creating user — is the visibility
boundary, per-worker identities would not isolate fleet writes on their own; the
remaining audit has to cover that, or the worker count needs a per-worker
tenant.

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
[Fault-Injection](../docs/infrastructure/Fault-Injection.md)).

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

### The initial JS bundle sits within 10 KB of its budget

[`.size-limit.json`](../web/.size-limit.json) caps the initial JS at 250 KB
gzipped excluding the lazy charts chunk. The investigations workspace took it to
roughly 240 KB — about one more feature of headroom before the gate refuses a
commit that is otherwise correct, and the person who hits it will be whoever
happens to add the next route rather than whoever spent the budget.

The workspace itself is already lazy and drew its evidence series as inline SVG
precisely to avoid a second charting dependency, so the remaining spend is in the
shared entry chunk, not in any one feature. Nothing has measured where.

**Pay-down trigger:** the next feature that lands within 5 KB of the cap, or the
first CI failure on the gate. Read the chunk breakdown `npm run build` prints,
split what the entry chunk is carrying that only one route needs, and move the
budget only after that has been done — raising the number is how a budget stops
being one.

### `reopen_window` has no per-rule override

An incident's auto-resolve hold is its rule's own grouping window, and there is
no per-rule override
([ADR-075](../docs/adr/ADR-075-incident-grouping-lifecycle-and-auto-resolve.md)
carries why). The cost is that a rule wanting to gather firings over one span
while holding its room for a different one cannot say so. No shipped rule wants
that, and none of the three curated shapes — fleet event, slow burn, recurrence —
needs it.

**Pay-down trigger:** a concrete rule that needs a hold differing from its
grouping window. That is a change to the relationship between the two grouping
axes, so it lands as a new ADR superseding ADR-075 on this point — not as a YAML
knob added to the catalogue grammar.

### Every control frame allocates the whole union

[`ControlMessage`](../server/internal/protocol/control.go) is one struct
carrying a field for every message type on the wire — 100 fields, 1 416 bytes —
and the decoder allocates one per frame. A heartbeat that sets three of those
fields costs the same as a discovery report that sets twenty, and Go rounds the
allocation to a size class, so the price rises in steps: eight bytes of new
alert fields took `BenchmarkCodec_DecodeControl` from 1 592 to 1 848 B/op by
crossing 1 408.

The union is what makes the hand-written encoder's field ordering byte-identical
to the Rust side ([ADR-060](../docs/adr/ADR-060-control-message-hand-written-encoder.md)),
so the fix is not a smaller struct but a different shape: a type per message,
decoded after the tag is read. That is a change to both language bindings and to
every golden fixture, which is why it is not being done for an allocation that
is measured in kilobytes per frame rather than megabytes.

[`control_size_test.go`](../server/internal/protocol/control_size_test.go) pins
the union inside its current size class so the next step is a decision rather
than a nightly benchmark surprise.

**Pay-down trigger:** the decode path shows up in a server CPU or allocation
profile taken under fleet load, or the union crosses another size class without
a message type to justify the fields that pushed it.

### Thirteen surviving mutants in the reconnect-backfill drain

The `rust-core-ml-backfill` shard never finished a nightly run before its split,
so its survivors were only ever seen partially. The run that reported furthest
([32212640976](https://github.com/volchanskyi/opengate/actions/runs/32212640976))
named thirteen, all in the tier walk now in
[`drain.rs`](../agent/crates/mesh-agent-core/src/ml/backfill/drain.rs): the
production `TierReader` rollup read, both per-tier cursor arms in
`BackfillCursors::get`, four band-arithmetic operators, and the phase-advance and
read-window comparisons in `next_batch`.

They are not one job. Some are ordinary test gaps — a resume test exists for the
1-minute tier and not for the other two, and a T1/T2 read through a real store
snapshot is never asserted. Others look equivalent on inspection: `emit_ok`'s
future-clock guard cannot be reached, because the phase band already bounds every
timestamp the reader is asked for, and the batch-window `+` behaves the same as
`*` under `.min(band_hi)` for any realistic clock. Separating the two needs the
shard to run to completion, which is what the split and the budget guard make
possible.

**Pay-down trigger:** the first nightly run in which both backfill shards
complete. Kill what a test can kill, and carve out what the run proves
equivalent with the reason written next to it.

### Alert state stays out of VictoriaMetrics

Three per-device edge series — `opengate_edge_alert_breach{rule,metric}` and
`opengate_edge_process_{cpu,mem}_percent{rank}` — sit outside the rule that keeps
central cardinality O(1), and are left as they are: of the four alternative
shapes considered, none is satisfying yet. Nothing is blocked by the deferral —
the per-device cap is enforced over the vitals set, and the aggregate rule
metrics
([ADR-076](../docs/adr/ADR-076-aggregate-platform-metrics-and-the-measured-alert-rate.md))
carry no device label.

**Pay-down trigger:** both of two facts, neither of which exists yet — the query
shape a real fleet board wants, once a technician has worked the incident API for
a while, and a measured alert volume at customer scale rather than the 0.2 per
device per day the ceilings were sized against. Together they turn the trade from
a judgement into arithmetic.

### One-year retention on alerts, evidence and incidents is declared, not swept

Alerts, evidence and incidents are declared to be kept for a year, and no
age-based deletion runs. The purge machinery in
[`internal/lifecycle`](../server/internal/lifecycle/) is device- and
customer-triggered rather than scheduled, so erasure still cascades — a purged
device or customer takes its alerts and evidence with it — and the aggregate
counters make row growth visible. What is missing is the sweep that would make
the declared year the actual one.

**Pay-down trigger:** measured growth projecting past ~10 GB/year, or a
compliance commitment that makes the year contractual. Build it against the
measured rate rather than the ~1.8 GB/year the estimate projects.
