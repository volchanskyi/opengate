# Technical Debt Register

<!-- Ordered by severity. Track only ACTIVE debt: when an item's pay-down trigger is met, delete it (the git history + the relevant ADR are the record). Do not keep resolved items or historical narrative here. -->
<!-- Last reviewed: 2026-08-31. -->

## Severity: Medium

### The nightly mutation score is below its baseline in go and rust

The go and rust legs sit under their baselines: rust 87.4 and go 85.5 against
88.5 and 88.2, so go has dropped more than the 2pp the check allows. The runs
that measured them were complete, so these are scores rather than an artefact of
a shard that went missing.

Reading the score at all needs a night that finishes, and the nightly stopped
producing one: a shard's own baseline suite has to pass before gremlins mutates
anything, and `TestASecondConnectionLeavesTheLiveOneAlone` failed on the
`go-domain-persistence` shard, taking the whole run's artefact set with it. That
case waited on the connected-agent count and then read the device row, and the
two move at different moments — the count on the accept path, the row when the
register frame is handled — so on a machine busy enough to widen the gap it read
the seeded status. It now waits for the machine to be online, which is the state
the reconnect is supposed to leave alone.

What moved is the denominator. Forty-seven commits landed the rules, alerts and
investigations programme between the baseline and that run — 434 files, roughly
66k lines — and the go figure carries 325 mutants in code no test reaches at
all, against 94 that a test reaches and fails to kill. The shape says new
surface arriving under-tested rather than existing tests weakening.

The web leg is out of it. A full local run reproduced the nightly's 82.4 exactly,
which made the gap addressable per file rather than per shard: the survivors
concentrated in the same rules and investigations surface the drop came from,
and covering seven of those files — `WhatItDoes`, `RuleList`, `RuleCoveragePanel`,
`DeviceLabels`, `MaintenancePanel`, `rule-store` and `device-tags-store` — took
the leg to 85.3 against its 85 floor. Nothing but tests changed. The margin
over the floor is thin enough that the next tranche of web surface will need the
same treatment as it lands rather than after the leg reds again.

`internal/rules` is the same job started on the go side: the two functions the
package left unreached — `Term.Comparator` and `ResumeRuleTenantWide` — are
covered, and with them the whole `wireTerms` path, which every test had been
skipping past because no fixture carried a rule with extra conditions.

**Pay-down trigger:** this is measured per shard, so it pays down per shard
rather than in one pass. Take the shards covering the new rules and alerts code,
kill what a test can kill, and carve out what the run proves equivalent with the
reason written next to it. A file's survivor list is the unit of work — reading
it off a local run costs less than a nightly and names the assertions that are
missing. The floor is the gate going green on its own rather than the baseline
being moved to meet it.

### The two larger fleets have not been built on staging

The three committed fleet sizes are all buildable in either venue, and the
nightly builds the smallest of them. The two larger ones are a deliberate
`workflow_dispatch` choice rather than a schedule, because staging's database
writes into the same node root production's does and nobody has yet measured what
a fleet four times the reference weighs. The performance stack weighs one on a
throwaway runner every night, which is the measurement that decision is waiting
on.

**Pay-down trigger:** a weighed fleet that fits inside the node's eviction
margin with room to spare. Schedule the larger sizes on staging then, or record
the number that says they cannot be.

### Load-test identities live in the default tenant

Every account a load run creates is made through the open registration endpoint,
which places it in the default tenant — the same tenant a technician's own
account is in. The run's identities are therefore mixed with people's, and the
only thing separating them is the marker in the address that
[`loadtest-cleanup.sh`](../scripts/loadtest-cleanup.sh) selects on.

A dedicated tenant is what this should be, and there is no way to ask for one:
the API creates customers, sites and users, and tenants exist only as rows a
migration seeded. So the run marks what it makes and removes it, which keeps the
environment clean without keeping the two populations apart while a run is in
flight.

**Pay-down trigger:** a tenant-creation API, for any reason. Give the load run
its own tenant, create its users and machines inside it, and reduce cleanup to
removing the tenant.

### The Always-Free processor grant is asserted by two gates and confirmed by none

[`compute.rego`](../policy/terraform/compute.rego) and the Terraform guards in
the `oke` and `compute` modules now both refuse above 2 processors / 12 GB, which
is the stricter of the two figures that were in the repository. Whether Oracle's
current grant is that or 4 / 24 is not settled: the OCI limits API exposes only
the paid service limit, so nothing queryable can answer it.

Holding both gates at the stricter figure is safe in the direction that matters —
a plan sized to it passes either gate — but it may be refusing capacity the
tenancy is entitled to, and nothing in the repository records which.

**Pay-down trigger:** the next time a second node or a larger shape is wanted.
Read the grant from the OCI console, set both gates to it, and record the figure
in an ADR. The block-storage grant is exactly full independently of this, so no
instance can be added until 50 GB is released whichever way it goes.

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

### Cross-restart TLS resumption is blocked on a rustls release

The agent's session store lives in memory, so a process restart —
auto-update, `RestartAgent`, watchdog rollback, a failed connect, a crash
— comes back to a full mTLS handshake. Ordinary connection drops re-dial
in process and are unaffected, which bounds this to restarts alone.

Persisting the store is not implementable on the pinned `rustls 0.23.43`:
the TLS 1.3 client session value is opaque by design — no public codec,
`pub(crate)` byte accessors, a zeroizing secret — for client-side forward
secrecy. The `0.24.0-dev.1` prerelease does expose `Tls13Session::encode`
and `from_slice`, and replaces the pointer-identity resumption gate with a
content-derived config hash, which is the shape a cross-process store
needs. No stable rustls carries it, and the newest quinn still requires
`rustls 0.23.x`.

**Pay-down trigger:** a stable rustls exposing client-session serialization
plus a published quinn that depends on it. Measure first whether a rebuilt
agent binary still matches its own persisted cache — the config hash is
derived partly from `TypeId`, and auto-update is the largest restart cause,
so a cache that misses on rebuilt binaries would close this by decision
rather than by code.

## Severity: Low

### The Chat tab is unreachable from any machine the browser stack can run

The tab is shown only for a machine reporting `RemoteDesktop`, and a Linux agent
reports the null capture implementation there — in production as much as in a
container. The browser stack runs Linux containers, so
[`chat.spec.ts`](../web/e2e/chat.spec.ts) is the one spec that describes its
machine rather than enrolling one, and says so in its own header.

**Pay-down trigger:** a Windows or macOS machine in the browser stack, for any
reason. Point the spec at it and delete the description.

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

77 Playwright tests across 21 spec files provision their own account
([`fixtures.ts`](../web/e2e/fixtures.ts)), and `workers: 1` is a deliberate fix
for `createAdminUser` racing on shared IAM state. Sharing one identity per worker
would cut account setup and is the prerequisite for raising the worker count.
The per-test navigation cost has been halved in the meantime (the token is
seeded via `addInitScript`, so an authenticated page is reached in one
navigation instead of two).

The per-spec audit is done, and it names two barriers rather than a diffuse
risk. Both are properties of the suite, not of the identities, so per-worker
accounts do not clear either on their own.

The binding one is `cannot remove last admin` in
[`security-permissions.spec.ts`](../web/e2e/security-permissions.spec.ts). It
empties the Administrators group down to a single member to reach the state it
asserts, and puts the members back in a `finally`. For the width of that window
the bootstrap operator is not an admin — and `createAdminUser` promotes every
admin fixture through exactly that credential
([`auth-helper.ts`](../web/e2e/helpers/auth-helper.ts)), so any of the 8 spec
files taking `adminUser` or `adminPage` that overlaps the window fails on a
promotion it had no part in. That is a serialization barrier against a third of
the suite at any worker count.

The second is [`device-site-dnd.spec.ts`](../web/e2e/device-site-dnd.spec.ts),
which holds two sites under the fixed names `Site A` and `Site B` and moves
`agent-b` between them, restoring both in `afterEach`. Sites are visible to the
whole customer while they are held, so the specs that render the fleet — the
screenshot baselines in
[`visual-regression.spec.ts`](../web/e2e/visual-regression.spec.ts) among them —
read them if they overlap.

The rest of the suite clears the audit on evidence.
[`device-list.spec.ts`](../web/e2e/device-list.spec.ts) names its sites by
timestamp, asserts only on the name it created, and deletes them `afterEach`.
[`restart.spec.ts`](../web/e2e/restart.spec.ts) fulfils the restart route in the
browser, so `agent-b` is read for its id and never actually restarted. The
membership tests in `security-permissions.spec.ts` assert `toContain` against a
user they registered themselves, which no sibling can perturb. Of the two real
machines, `agent-a` is read-only to
[`file-manager`](../web/e2e/file-manager.spec.ts),
[`hardware`](../web/e2e/hardware.spec.ts),
[`inventory`](../web/e2e/inventory.spec.ts) and
[`session-terminal`](../web/e2e/session-terminal.spec.ts), and `agent-b` is
written by `device-site-dnd.spec.ts` alone.

Two pieces of that audit are enforced rather than pending. The
`globalTeardown` in [`global-teardown.ts`](../web/e2e/global-teardown.ts) fails
any run that leaves a group behind, and
[`fleet-stub.ts`](../web/e2e/helpers/fleet-stub.ts) gives the specs that assert
an empty fleet a way to supply that emptiness instead of reading shared state.
Both hold at any worker count. Since the tenant — not the creating user — is the
visibility boundary, raising the count needs the last-admin test in a lane of its
own and `device-site-dnd.spec.ts` holding uniquely-named sites on a device it
owns, or it needs a per-worker tenant.

**Pay-down trigger:** E2E wall time becomes a merge-latency problem. The audit no
longer gates the change; the two barriers above are what the work consists of.

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

