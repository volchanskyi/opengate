# Acceptance-Test Tier — Specification

**Every figure below was read off this repository, a CI run, or a command run
against it on 2026-08-20.** Anything not measured is in §12 and is labelled as
such. Where a number came from a command, the command is named so it can be
re-run.

---

## 1. Confirmed state

### 1.1 What the test suite is, measured

| Tier | Files | `func Test` | Local wall clock | CI |
|---|---|---|---|---|
| `server/internal/api` | 76 | 220 | 56.2 s test / 61.5 s total | 294 s (whole `./internal/...` job) |
| `server/tests/integration` | 42 | 82 | 15.3 s test / 35.4 s total | 115 s |
| `web/e2e` (Playwright) | 21 specs | 77 | — | 214 s |

Local numbers from `go test -race -count=1 ./internal/api/` and
`./tests/integration/` with `POSTGRES_TEST_URL` and `VICTORIAMETRICS_TEST_URL`
exported. CI numbers from run `32343393649` on `dev`.

`internal/api` is not a unit tier. Its tests drive the **routed** server:
`doRequest` in [`helpers_test.go:168-199`](../../../server/internal/api/helpers_test.go)
builds an `httptest.NewRequest` and calls `srv.ServeHTTP`, so every request
passes the whole chi middleware chain against a real Postgres schema. It reaches
**59 of the 63 paths** in [`api/openapi.yaml`](../../../api/openapi.yaml).
`tests/integration` reaches 29. The browser suite reaches 51.

The middle of the pyramid is therefore both wide and fast, and no new tier is
needed. What is missing is five specific things.

### 1.2 Finding 1 — no test stands the product up

`ServerConfig` ([`api.go:172-222`](../../../server/internal/api/api.go)) declares
**44 fields**. Counting every composite literal of it in the tree:

| | Sites | Fields wired |
|---|---|---|
| `ServerConfig` literals | **41** | production `main.go`: **41** · richest test (`handlers_rules_access_test.go`): **22** · the default `newTestServer`: **18** |
| `AgentServerConfig` literals | **12** | production: **19 of 19** · richest test (`investigations_test.go`): **10** |

Seven agent-side ports are wired in production and by **no test at all**:
`AlertRules`, `Inventory`, `Metrics`, `Processes`, `RuleCoverage`, `Telemetry`,
`Tombstones`.

Forty-one hand-written wirings is forty-one chances to diverge, and the
divergence is silent: chi's `middleware.Recoverer`
([`api.go:407`](../../../server/internal/api/api.go)) turns a missing port into a
500 rather than a crash, so a route whose port the harness forgot simply
answers 500 and nobody writes a test against it.

### 1.3 Finding 2 — the seams that carry the product's value are joined by nothing

Three chains, each with good coverage on both banks and no bridge.

**A machine joins the fleet.** `grep -rn "enrollment-tokens\|/enroll/"
server/tests/integration/` returns **nothing**. `newAgentTestEnv`
([`agentapi_test.go:81`](../../../server/tests/integration/agentapi_test.go))
pre-seeds a device row and mints a certificate straight from the certificate
manager. The chain a real machine walks — administrator mints a token, agent
generates a certificate request, server signs it, agent connects, device row
appears — is proven by no test.

**A machine reports its readings and the technician sees them.**
`MetricWindow` appears in **zero** files under `server/tests/integration/`. The
write half is tested in `internal/agentapi` against in-process fakes; the read
half in `internal/api` against `fakeMetricsReader`
([`handlers_device_metrics_test.go:37`](../../../server/internal/api/handlers_device_metrics_test.go));
the VictoriaMetrics client in `internal/telemetry` against a real store. Four
halves, no whole.

**This exact gap costs money.** Commit `532f7abe` is the evidence: the ingest
counter fired before any handler decided whether it had anything to persist, and
the fleet measured thousands of metric windows received, counted, never written,
and never dropped. Every tier was green for the whole of it. The guard that now
holds that seam is an accounting invariant — `ingested` minus `drops` equals
persisted — which is the right shape for the mechanism and is still not a
statement that a technician can see the minute the machine reported. A store that
accepted a reading and a reader that returned nothing would balance that ledger
perfectly.

**An alert becomes an incident a technician closes.**
`TestTriagePathFromAgentAlertToResolution`
([`investigations_test.go:159`](../../../server/tests/integration/investigations_test.go))
is the repository's one deliberately joined test, and it stops short: the
technician half calls `env.alerts.Queue(...)` and `env.alerts.Transition(...)`
directly on the store, never through HTTP. It has to, because no harness has both
a QUIC listener and a wired API server. If the API refused that resolution on an
authorisation or tenancy ground, this test would still pass.

### 1.4 Finding 3 — the browser suite cannot begin at a machine

[`docker-compose.test.yml`](../../../deploy/docker-compose.test.yml) runs a server
and a database and **no agent**. Its `command` omits `-quic-listen` and its
`ports` publishes only `8080`, so the QUIC listener the server image already
declares (`EXPOSE … 9090/udp` in the [`Dockerfile`](../../../Dockerfile)) is not
reachable.

Consequently **9 of 21 specs** call `page.route`, and **six of them fabricate a
whole device** — `devices/{id}`, its hardware, its inventory, its logs, its
sessions, its restart endpoint:

| Spec | Tests | What it stubs |
|---|---|---|
| `session-terminal.spec.ts` | 4 | device, hardware, sessions, session create |
| `file-manager.spec.ts` | 3 | device, hardware, sessions, session create |
| `restart.spec.ts` | 3 | device, hardware, logs, sessions, restart |
| `chat.spec.ts` | 2 | device, hardware, sessions, session create |
| `device-site-dnd.spec.ts` | 2 | devices, sites, device, inventory |
| `hardware.spec.ts` | 1 | device, hardware, logs, sessions |
| `inventory.spec.ts` | 1 | device, inventory, logs, sessions |

Sixteen tests. "A technician opens a terminal on the customer's machine" is
today an assertion that the browser renders a tab, against a server that is not
there.

### 1.5 Finding 4 — 32 tests under `server/tests/` never run in CI

The gauntlet runs the whole tree —
`go test -race -count=1 -timeout 5m ./tests/...`
([`precommit-gauntlet.sh:314`](../../../scripts/precommit-gauntlet.sh)). CI runs
`./tests/integration/` only
([`ci.yml:280`](../../../.github/workflows/ci.yml)), and `grep -rn "vmbackfill\|
vmcardinality\|vmramseries" .github/workflows/` returns nothing.

| Package | `func Test` | Runs in CI |
|---|---|---|
| `tests/integration` | 82 | yes |
| `tests/vmramseries` | 16 | **no** |
| `tests/loadtest` | 10 | **no** |
| `tests/vmcardinality` | 4 | **no** |
| `tests/vmbackfill` | 2 | **no** |

A new package under `server/tests/` inherits this hole silently.

### 1.6 Finding 5 — the two Go middle tiers have no stated seam

Classifying every file in `server/tests/integration/` by whether it references a
real QUIC peer, a real socket or a WebSocket:

| Requirement | Tests |
|---|---|
| Needs a real QUIC agent | 25 |
| Needs a real socket / WebSocket | 14 |
| **Needs neither** | **47** |

Forty-seven of eighty-two assert nothing an in-process test could not assert.
Two clear examples: `postgres_native_test.go` (7 tests) pins pgx TIMESTAMPTZ /
JSONB / UUID semantics and never touches HTTP; `signaling_test.go` (3 tests)
constructs a `signaling.Tracker` and asserts on it, importing no database and no
transport. Both are unit tests at an integration address, because nothing says
where a test belongs.

### 1.7 The shape three tests already have

Three tests in the tree are acceptance tests in every respect except their name
and their address:

- `TestUpdatePublishAndPush` — administrator publishes a signed manifest, the
  machine receives it over QUIC, acknowledges, and the database records it.
- `TestDeviceLogsBroker_RoundTripRedactedAndAudited` — an administrator's pull
  blocks until the online machine answers, lines come back redacted, nothing is
  persisted, and the access is audited.
- `TestTriagePathFromAgentAlertToResolution` — the triage path, minus the HTTP
  half.

This programme names that shape and finishes it.

Two structural guards in the tree are the model for the completeness gate:
`TestEveryTenantTableIsProbed`
([`tenant_isolation_test.go`](../../../server/internal/db/tenant_isolation_test.go))
reads the schema back and insists the probes cover it, and
`TestCountedIngestTypesMatchDispatch`
([`conn_accounting_guard_test.go:131`](../../../server/internal/agentapi/conn_accounting_guard_test.go))
reads a dispatch switch out of the package's own syntax tree.

---

## 2. Decisions locked

| # | Decision | Ground |
|---|---|---|
| 1 | Build the harness, the capability suite **and** the completeness gate; also restructure the two Go middle tiers | user |
| 2 | **Extract the composition root** into an importable package; `main()` and the harness call the same wiring | user |
| 3 | **Put a real agent in the browser stack** and de-stub the six fabricated-device specs | user |
| 4 | The capability tests live in a **new `server/tests/acceptance` package**, and CI's hole (§1.5) is closed in the same programme | user |
| 5 | No Gherkin, no YAML scenario layer. Go tests, operator-language names, domain-language assertion messages | the domain is technical and its readers are engineers, so a business-readability layer buys nothing and costs a framework |
| 6 | Real Postgres everywhere. Double only what sits **outside** the product: Intel AMT hardware, web-push delivery, the GitHub manifest feed | a hand-written repository double drifts from real semantics — `ON CONFLICT`, ordering, transaction isolation, exact error values — which is a fidelity loss this product cannot take. `testpg` already makes a real database unconditional ([ADR-029](../../../docs/adr/ADR-029-test-determinism-no-silent-skips.md)) |

### On decision 2 — where the seam falls

`main()` is 480 lines and Go forbids importing `package main`, so no test can
call production's wiring. The extraction splits it on a line that makes the new
package fully testable:

- `main()` keeps **reading the world** — flags, environment variables, exit
  codes, listener startup, background loops.
- `app.Build(cfg Config) (*Assembly, error)` keeps **assembling the product** —
  every repository, every port, both servers. No flag parsing, no `os.Exit`, no
  listeners.

That matters for a gate: `internal/app` falls inside the Go coverage gate
(`./internal/...`, [`ci.yml:221`](../../../.github/workflows/ci.yml)), and a
`Build` free of flags and exits is coverable to the last line by the harness that
calls it.

---

## 3. Scope

### In scope

- One importable composition root; `main()` reduced to reading the world.
- A `server/tests/acceptance` package: a product harness, two actors, and one
  outcome test per product capability.
- A completeness gate binding [`docs/product/`](../../../docs/product/) chapters to
  acceptance tests, in both directions.
- Closing the three measured seam holes (§1.3) and the enrolment hole.
- Moving the 47 transport-free tests out of `tests/integration`, and a guard that
  keeps them out.
- Widening CI to the whole `server/tests/` tree (§1.5).
- An agent image and two agent services in the browser stack, with QUIC exposed
  and real enrolment.
- Deleting the fabricated-device stubs in six Playwright specs.
- Docs, an ADR, and the three state files.

### Out of scope

- Any change to product behaviour. This programme adds no endpoint, no flag and
  no field to the wire protocol.
- A test-only endpoint, flag or environment variable in the shipped server. The
  `OPENGATE_TEST_MODE` variable in the compose file, which no Go source reads, is
  deleted rather than given a meaning.
- Growing the Playwright suite. Its test count does not rise; sixteen of its
  tests become true.
- Windows or macOS agents in the browser stack. Linux agents support Terminal and
  File Manager only ([`platform-linux/src/lib.rs`](../../../agent/crates/platform-linux/src/lib.rs)),
  and desktop capture is `NullCapture` there in production too, so a Linux
  container is not a reduced fidelity.
- Asserting on telemetry charts from the live agent in the browser. The central
  vitals cadence is a 60-second window
  ([ADR-065](../../../docs/adr/ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md)),
  which is too slow for a browser spec; the Go tier owns that outcome by driving
  the control stream directly.
- Load, performance and fault families. Those belong to
  [`performance-test-strategy-revision.md`](../performance-test-strategy-revision.md)
  and [`Fault-Injection.md`](../../../docs/infrastructure/Fault-Injection.md).

---

## 4. Domain model — what exists, how it collaborates, where the boundaries are

### 4.1 The two actors, and nothing else

An acceptance test may speak through exactly two doors, because a real
installation has exactly two:

| Actor | Door | Realised as |
|---|---|---|
| **Technician** | the HTTP API on `127.0.0.1:0` | a client holding a JWT, issuing the same requests the browser issues |
| **Machine** | the QUIC control stream | a peer speaking [`internal/protocol`](../../../server/internal/protocol) — the same encoder the Rust agent's wire is proven identical to by the golden tests |

A test that reaches past those doors into a repository is not an acceptance test.
The one exception is **arrangement with no door**: where the product offers no
way for an operator to create a precondition, the harness may seed it, and the
seeding helper is named so the exception is visible in the test's own text.

The Go `Machine` is not a substitute for the Rust agent. It is a peer on the same
wire, and the bidirectional golden tests
([ADR-016](../../../docs/adr/ADR-016-bidirectional-goldens-and-sidecars.md)) are
what make that claim true rather than hopeful. The Rust agent's own behaviour is
proven by Rust tests and, after this programme, by the browser stack.

### 4.2 Objects

| Object | What it is | Realised as |
|---|---|---|
| **Assembly** | the whole product, wired | `app.Assembly` — the API server, the agent server, the relay, the purge orchestrator, the background-loop dependencies |
| **Product** | one Assembly with listeners, a live database and doubled outside edges | `acceptance.Product`, built per test |
| **Technician** | one authenticated operator inside one customer | `Product.Technician(t, org)` |
| **Machine** | one enrolled device holding a control stream | `Product.Machine(t, site)` |
| **Capability** | one chapter of [`docs/product/`](../../../docs/product/) | a row in the binding table |
| **Outcome** | one sentence a customer would recognise | one `Test…` function, named for the sentence |

### 4.3 Relationships

1. A Product owns exactly one database schema, one data directory and one
   Assembly. Products never share state, so every acceptance test may run in
   parallel.
2. A Technician belongs to exactly one customer inside one tenant. Asking for a
   second Technician in a second customer is how a tenancy outcome is stated.
3. A Machine belongs to exactly one site, and reaches the Product only through
   its control stream.
4. A Capability is proven by one or more Outcomes. Every chapter has at least
   one; every Outcome names exactly one chapter. The gate checks both directions.
5. An Outcome asserts only on what an actor can observe. "The row is in the
   table" is not an outcome; "the device page shows it" is.

### 4.4 Ownership boundaries

| Component | Owns | Must not |
|---|---|---|
| `internal/app` | assembling the product from resolved configuration | read flags or the environment, call `os.Exit`, open a listener |
| `cmd/meshserver` | reading the world, starting listeners, background loops | wire a port |
| `tests/acceptance` | outcomes, in the operator's words | reach past the two doors except to arrange |
| `tests/integration` | transports and seams: QUIC, sockets, WebSockets, two servers | hold a test that needs none of them |
| `internal/<pkg>` | that package's behaviour, including its HTTP surface | depend on another package's transport |
| `web/e2e` | the browser | fabricate a device that the stack could supply |

---

## 5. Where the change sits

```
                       ┌───────────────────────────────┐
   cmd/meshserver ────►│  internal/app  · Build(cfg)   │◄──── tests/acceptance
   (flags, env,        │  the ONE composition root     │      (Product harness)
    listeners, loops)  └───────────────┬───────────────┘
                                       │
                    ┌──────────────────┼──────────────────┐
                    ▼                  ▼                  ▼
              api.Server        agentapi.AgentServer   relay.Relay
                    │                  │
        HTTP on :0  │                  │  QUIC on :0
                    ▼                  ▼
              Technician            Machine          ← the only two doors
```

**Dependencies this adds:** none. No new module, no new container image in the Go
path. `internal/app` imports what `main.go` already imports.

**Dependencies the browser half adds:** one agent image built from the existing
`x86_64-unknown-linux-musl` target that
[`release-agent.yml`](../../../.github/workflows/release-agent.yml) already
produces, and two compose services.

**What the harness doubles, and why each is genuinely outside:**

| Doubled | Why it cannot be real in a test |
|---|---|
| `amt.Operator` | Intel management hardware answering on its own network path |
| `notifications.Notifier` | a browser push service on the public internet |
| GitHub manifest sync (`GitHubRepo`) | a third-party API |

Everything else is real: Postgres via
[`testutil.NewTestStore`](../../../server/internal/testutil/testutil.go), the
certificate manager over a temporary directory, the relay, the signalling
tracker, the rule catalogue compiled into the binary, and — for the readings
outcome — VictoriaMetrics via
[`testvm`](../../../server/internal/testvm/testvm.go).

---

## 6. Quality bars and non-functional requirements

**Correctness of the harness.** The harness must fail loudly, never quietly.
A port the harness does not wire must make the test that needs it fail with a
message naming the port, not return a 500 that an assertion then pins. `app.Build`
returns an error for a missing required dependency rather than constructing a
server that answers 500.

**Speed.** The acceptance package must stay inside the current integration
budget: **≤ 60 s wall clock locally** with shared Postgres and VictoriaMetrics
exported. The measured baseline it must not blow past is 35.4 s for
`tests/integration` today. Each acceptance test calls `t.Parallel()`; per-test
schema isolation already makes that safe.

**Determinism.** No fixed sleeps. Synchronisation is `require.Eventually` on an
observable outcome, which is the discipline the integration tier already
converted seventeen sleeps to. No test may depend on another's state, and no test
may depend on run order.

**Determinism in the browser, with a live agent.** Hardware and inventory values
differ per runner, so specs assert on **shape and presence**, never on values.
Container hostnames are pinned in compose so device names are stable. Two agent
services exist so a spec may take one offline without breaking every other spec;
`agent-b` is the expendable one.

**Security.** No certificate private key leaves the test process. The browser
stack's agents enrol through the real public enrolment endpoint with a token
minted at bring-up, so no key is copied and no test-only bypass is added. The
bootstrap credentials stay confined to the disposable stack, whose database is on
`tmpfs`.

**Long-term maintainability.** The number of composition roots goes from
**41 to 1**. A new port added to `ServerConfig` is wired once. A capability added
to the product without an outcome test fails the suite rather than being noticed
later. The seam guard means a test's address states what it needs, so a reader
choosing where to put a new test has a rule instead of a precedent.

**Repository constraints that bind the implementation.**

- **The acceptance package holds no non-test Go file.** Harness, actors and
  binding table all live in `_test.go` files, the way all 42 files of
  `tests/integration` already do. That keeps the package out of the mutation
  partition check ([`mutation-workflow.test.sh:330`](../../../scripts/tests/mutation-workflow.test.sh)),
  out of `.gremlins.yaml`, and out of every coverage list. Zero new bookkeeping.
- **`internal/app` is a new non-test package and needs a mutation decision.**
  Every non-test `.go` file under `server/` must belong to exactly one mutation
  unit or a global exclude. `cmd/meshserver/` is globally excluded today on the
  stated ground that "a mutant there changes how the process is assembled rather
  than what any behavior does" ([`.gremlins.yaml`](../../../server/.gremlins.yaml)).
  The code is moving, so the same ground applies — but it is **re-earned, not
  inherited**: step AT-1 measures the mutants first and records the number.
- **`internal/app` is inside the Go coverage gate.** Keeping flags and exits in
  `main()` is what makes 80% reachable there; the harness covers the rest.
- **`go-arch-lint` excludes `_test.go`** ([`.go-arch-lint.yml`](../../../server/.go-arch-lint.yml)),
  so the harness imports freely. `internal/app` lands in the unconstrained `other`
  component.
- **Test-first, with no bypass.** Every source change needs a failing test on the
  branch first ([`rules/tdd.md`](../../rules/tdd.md)). Anything under a `tests/`
  path already classifies as a test file for the gate
  ([`tdd-check.sh:24`](../../../scripts/tdd-check.sh)).
- **New `scripts/tests/*.test.sh` files must be mode 100755**, or the gauntlet's
  shell-tests step fails.
- **No silent skips.** Every acceptance test runs on every machine
  ([`rules/tests-determinism.md`](../../rules/tests-determinism.md)); a missing
  Postgres or VictoriaMetrics is provisioned, never skipped around.
- **Docs live in three trees.** Testing belongs to
  [`docs/infrastructure/Testing.md`](../../../docs/infrastructure/Testing.md); a
  product chapter may not link a `scripts/` or `.github/` path
  ([`docs-seam.test.sh`](../../../scripts/tests/docs-seam.test.sh)).

---

## 7. The capability map

One row per chapter of [`docs/product/`](../../../docs/product/). "Today" is what
the tree proves now; "Outcome" is the sentence the acceptance test asserts.

| Chapter | Outcome to state | Today |
|---|---|---|
| **Agent Deployment** | An administrator mints an enrolment token for Contoso's *Head Office* site; a machine enrols with it and appears online in the device list | **nothing joins it** — no test references the enrolment endpoints (§1.3) |
| **Fleet and Devices** | A technician switching to Fabrikam sees Fabrikam's machines and not Contoso's, and the dashboard counts equal the list | handler tests with stub readers |
| **Remote Sessions** | A technician opens a terminal on an online machine, sends a command, and the machine receives the bytes | `TestSessionLifecycle_CreateAndRelay` — **already this shape**, in protocol language |
| **Device Health** | A machine reports a minute of readings; the technician reads that minute back on the device page | **nothing joins it** — `MetricWindow` absent from the joined tier (§1.3) |
| **Alerts and Rules** | A rule reaches a machine, the machine breaches it, and an alert carrying its evidence arrives | agent-side only; the rule never travels in a test |
| **Rule Administration** | An administrator retunes a rule for Contoso; Contoso's machines receive the new threshold and Fabrikam's do not | handler tests with stub coverage readers |
| **Investigations** | An alert becomes an incident a technician picks up and closes with a cause code | `TestTriagePathFromAgentAlertToResolution` — **bypasses HTTP** (§1.3) |
| **Endpoint Logs** | A technician pulls a machine's log, receives redacted lines, nothing is stored, and the access is audited | `TestDeviceLogsBroker_RoundTripRedactedAndAudited` — **already this shape** |
| **Intel AMT** | A technician powers on an unresponsive AMT machine from its device page | property + not-connected paths only; the hardware is genuinely outside |
| **Agent Updates** | An administrator publishes a signed build and the machine acknowledges it | `TestUpdatePublishAndPush` — **already this shape** |
| **Tenancy and Access** | A member of Fabrikam cannot read Contoso's machines by any route, including with a guessed id | proven per-route and at the database; not stated as one outcome across the surface |
| **Data Erasure** | Deleting a machine removes it from every store and its agent stops being trusted | store-level; the connected-agent case is not joined |

Four of the twelve are already in the right shape and only need moving and
renaming. Four have a measured hole. Four are covered on both banks with no
bridge.

---

## 8. Edge and error cases the outcomes must carry

These are the states an RMM actually meets. Each is named here so the
implementation cannot quietly omit it; each belongs to the outcome above it.

**Deployment and identity**

1. **A token used twice.** Contoso's technician pastes the install command into
   two machines. The second enrolment must be refused or must produce a second
   distinct device — never silently overwrite the first.
2. **A cloned virtual machine.** An image is captured after enrolment and
   redeployed, so two hosts hold the same identity and both connect. The server
   must resolve to one live connection; the loser must not leave a half-open
   stream that a session request can be routed to.
3. **A machine that reconnects with a new certificate** after a rebuild. It must
   arrive as the same device, not a duplicate.

**Time and catch-up**

4. **A machine whose clock is wrong.** A laptop returning from a suspended
   virtual machine stamps readings hours out. The stamp is pulled to the nearer
   bound and the reading is still persisted — a clamp is not a drop.
5. **A machine that was offline for a day.** Its stored readings arrive as
   backfill, and the ones outside retention are discarded with a named reason
   rather than silently.
6. **Readings that arrive with nothing in them.** The empty-payload case is the
   one that cost the fleet (§1.3): every received message either lands or says
   why not.

**Tenancy and visibility**

7. **A technician with an id they should not have.** Fabrikam's technician
   guesses a Contoso incident id. The answer must be indistinguishable from "no
   such incident" — a different status code leaks the existence of the row.
8. **A customer filter that narrows but does not permit.** Both customers sit
   inside one tenant, so nothing is refused and a wrong query simply shows
   Contoso's estate to somebody looking at Fabrikam's. That failure is silent and
   must be asserted for directly.
9. **The last administrator.** Emptying the Administrators group must be refused,
   or the installation locks itself out.

**Live work**

10. **The machine disappears mid-session.** A technician is in a terminal when
    the endpoint drops off the network. The session must end, the row must be
    cleaned up, and the browser must be told — not left waiting.
11. **A session for a machine that is offline.** Refused with a reason a
    technician can act on.
12. **A machine under maintenance.** A machine in a maintenance window that
    breaches a rule must not open an incident, and must resume raising them when
    the window closes.

**Detection**

13. **A rule that is wrong.** The stop switch takes effect on machines already
    carrying the rule, without a deploy.
14. **A rule that is expensive.** A machine at its alert ceiling stops raising
    and says so, rather than flooding.
15. **A rule a machine cannot evaluate.** Coverage says "unsupported" durably,
    and the technician sees that rather than an unexplained silence.

**Erasure**

16. **Deleting a machine whose agent is connected.** The row goes, the agent is
    deprovisioned, and its next connection is refused.
17. **Erasure with the readings store unreachable.** Recorded as an unfinished
    erasure, not as a success. (Already a known debt; the outcome test pins the
    behaviour that exists.)

**Failure of the harness itself**

18. **A port the harness does not wire.** Fails naming the port. Never a 500 an
    assertion can be written against.
19. **A capability chapter with no outcome test.** Fails the suite, naming the
    chapter.
20. **An outcome test naming a chapter that does not exist.** Fails the suite —
    the binding is checked in both directions.

---

## 9. Implementation steps

Ordered. Each is independently testable and independently committable. Steps
carry the workstream ids the micro-plans will use.

### AT-0 — Close the CI hole (independent, do first)

Widen the Go integration job to `./tests/...` and give it the Postgres and
VictoriaMetrics the newly-visible packages need. Add a shell test asserting the
package list CI runs equals the package list the gauntlet runs, so the two cannot
drift again. The 32 tests CI does not currently run either go green or reveal
something; either result is worth having before anything else lands.

### AT-1 — One composition root

1. `server/internal/app`: `Config` (resolved values only), `Assembly`, `Build`.
   Move the wiring out of `main()` unchanged.
2. `main()` keeps flags, environment, listeners, background loops, exit codes.
3. A test that constructs `Build` twice and asserts the second Assembly shares no
   mutable state with the first.
4. Measure `internal/app`'s mutants; record the number; decide the carve-out
   against that number and write the justification next to the entry.
5. Verify `main_test.go` still passes and the binary still boots
   (`make build`, then the smoke test).

### AT-2 — The acceptance harness and the three exemplars

1. `server/tests/acceptance/harness_test.go`: `Product`, over `app.Build`, with
   real Postgres, temporary data directory, HTTP and QUIC on `127.0.0.1:0`, and
   the three outside edges doubled.
2. `technician_test.go` and `machine_test.go`: the two actors.
3. Move `TestUpdatePublishAndPush`,
   `TestDeviceLogsBroker_RoundTripRedactedAndAudited` and
   `TestTriagePathFromAgentAlertToResolution` in, renamed to their outcome
   sentences — and **make the triage path go through HTTP**, closing the bypass
   §1.3 records.
4. Assert the harness fails loudly on an unwired port.

### AT-3 — The capability map and its gate

1. `capabilities_test.go`: a table binding each `docs/product/*.md` chapter to
   the acceptance test functions that prove it.
2. A guard that reads the package's own syntax tree and fails when a chapter has
   no test, a named test does not exist, or a `Test…` function in the package is
   absent from the table. Same shape as `TestEveryTenantTableIsProbed`.
3. Land it failing for the eight unproven chapters, so AT-4 has a red bar to
   drive against.

### AT-4 — Close the measured holes

One outcome test per chapter, each carrying the edge cases §8 assigns it. Order
by what is most exposed:

1. **Device Health** — the readings round-trip, over real VictoriaMetrics. This
   is the one a shipped defect walked through.
2. **Agent Deployment** — mint, enrol, connect, appear.
3. **Tenancy and Access** — one outcome across the whole surface, including the
   guessed id and the narrowing filter.
4. **Alerts and Rules** and **Rule Administration** — a rule that travels, a
   tuning that lands on one customer only, a stop switch that takes effect.
5. **Data Erasure** — with the agent connected.
6. **Fleet and Devices**, **Remote Sessions**, **Intel AMT** — bring the existing
   shape up to the outcome standard.

### AT-5 — The seam between the Go tiers

1. Classify all 82 tests in `tests/integration` by transport requirement.
2. Move the 47 that need none into `internal/<pkg>` as `package <pkg>_test`,
   which keeps them black-box; the repository already uses that convention in 14
   packages. `postgres_native_test.go` → `internal/db`; `signaling_test.go` →
   `internal/signaling`; the security-group, admin and auth-edge files →
   `internal/api`.
3. `scripts/tests/test-tier-placement.test.sh`: every `_test.go` under
   `tests/integration/` must reference a transport entry point from a named list,
   and no file under `tests/acceptance/` may import a repository package outside
   the arrangement helpers.
4. Re-measure both packages' wall clock; record the numbers.

### AT-6 — A real agent in the browser stack

1. An agent image: `FROM alpine`, the existing `x86_64-unknown-linux-musl`
   binary, no runtime dependencies. The agent already detects
   `LinuxRuntime::Container` and falls back to `NullServiceLifecycle` without
   systemd, so this is a supported shape, not a workaround.
2. CI: build the binary once in a job carrying the existing Rust cache, upload it
   as an artifact, have the e2e job download it. Measured cold locally at
   **1 m 17 s** for a 24.6 MB static binary; a four-core runner will be slower, so
   the artifact hand-off is what keeps the e2e job's 214 s from growing.
3. Compose: add `-quic-listen` to the server command, publish `9090/udp`, add
   `agent-a` and `agent-b` with pinned hostnames, and delete the
   `OPENGATE_TEST_MODE` variable no Go source reads.
4. Bring-up: `make e2e` starts database and server, mints a bootstrap
   administrator and an enrolment token for a fixture site, starts both agents
   with that token, and waits for two devices online before Playwright runs.
   `playwright.config.ts`'s `webServer.command` calls the same script, so the two
   paths cannot diverge.
5. `global-teardown.ts` exempts the fixture site by name and still fails on
   anything else, keeping the leak attribution it exists for.
6. A shell test asserting the compose stack publishes QUIC and that the bring-up
   script's endpoints exist in `openapi.yaml` — the same drift guard
   [`api-endpoint-drift.test.sh`](../../../scripts/tests/api-endpoint-drift.test.sh)
   already applies to k6 and the smoke test.

### AT-7 — De-stub the six specs

Delete the fabricated-device `page.route` calls in `session-terminal`,
`file-manager`, `restart`, `chat`, `device-site-dnd`, `hardware` and `inventory`;
point them at the enrolled device. Assertions move from values to shape. The
`restart` spec targets `agent-b`. Test count does not change; sixteen tests stop
being fiction.

### AT-8 — Docs, decision record, state files

Rewrite the tier table in
[`Testing.md`](../../../docs/infrastructure/Testing.md) to state the seam and the
two doors; add the acceptance tier and the completeness gate; update the
Playwright section now that the stack carries agents. Write the ADR — **081, or
the next free number if
[`performance-test-strategy-revision.md`](../performance-test-strategy-revision.md)
lands first**. Add the `decisions.md` row, the `phases.md` Completed rows, and any
debt this uncovers. Archive this plan in the commit that lands the last
workstream.

---

## 10. The three dimensions

**Core logic.** One composition root and the Assembly it returns. A Product
harness that stands it up over a real database. Two actors and no third door. A
capability binding checked in both directions out of the syntax tree. A placement
rule for the Go tiers, enforced. A browser stack that carries real machines.

**Scope boundaries.** `internal/app` assembles and never reads the world.
`cmd/meshserver` reads the world and never wires a port. `tests/acceptance` states
outcomes and reaches only the two doors. `tests/integration` holds what needs a
transport and nothing else. `internal/<pkg>` holds everything else, black-box
where it was black-box. `web/e2e` owns the browser and stops fabricating devices.
No product behaviour changes; no test-only affordance enters the shipped server.

**Definition of done.**

- `internal/app.Build` is the only place a port is wired; `grep` finds one
  `api.ServerConfig{` and one `agentapi.AgentServerConfig{` outside test
  arrangement.
- Every chapter in `docs/product/` is named by at least one acceptance test, and
  every acceptance test names exactly one chapter; both directions gate.
- The readings round-trip is asserted from the machine's report to the
  technician's read, over a real metrics store.
- Enrolment is asserted from token to online device.
- The triage path is closed through HTTP, with no direct store call on the
  technician half.
- Every edge case in §8 is carried by a named test.
- Every `_test.go` under `tests/integration/` needs a transport, proven by a
  guard.
- CI runs every package under `server/tests/`, proven by a guard that compares
  CI's list to the gauntlet's.
- The browser stack carries two enrolled machines; no spec fabricates a device;
  the spec count is unchanged.
- The acceptance package runs in ≤ 60 s locally with shared services, every test
  parallel, no fixed sleeps.
- Three consecutive full runs give the same result.
- `Testing.md`, the ADR, `decisions.md`, `phases.md` and `techdebt.md` are
  current; this plan is archived.

---

## 11. Verification

- **AT-0:** delete a package's test file locally and confirm CI's list guard
  fails; run CI and confirm the 32 tests appear in the job summary.
- **AT-1:** `make build` and the smoke test still pass; a port removed from
  `app.Build` makes an acceptance test fail naming the port, not a 500.
- **AT-2:** the three moved tests pass unchanged in meaning; the triage test now
  fails if the API refuses the resolution.
- **AT-3:** add a chapter file with no test and confirm the gate fails naming it;
  add a test absent from the table and confirm the gate fails the other way.
- **AT-4 (Device Health):** the regression proof for `532f7abe` — make the ingest
  path drop an empty payload silently and confirm the readings outcome goes red.
  A guard that would not have caught the defect that shipped is not the guard.
- **AT-5:** `go test ./tests/integration/` and `./internal/...` both green after
  the move; the placement guard fails when `signaling_test.go` is put back.
- **AT-6:** `make e2e` from a cold tree brings up two online devices; record the
  e2e job's before/after duration in CI and state it.
- **AT-7:** stop `agent-a` and confirm the de-stubbed specs fail — proof they
  read the real machine rather than a leftover stub.
- **Whole:** `/precommit`, then a full CI run, then a second and third to confirm
  the verdict is stable.

---

## 12. Not confirmed

Read off no system; none blocks AT-0 through AT-5.

1. **The e2e job's growth from a real agent.** The binary's cold build is measured
   (1 m 17 s on a sixteen-core machine); a four-core GitHub runner's figure, and
   the artifact download and image build on top, are not. AT-6 records them in the
   first run rather than assuming them.
2. **Whether two agent containers fit the runner comfortably.** The agent samples
   at 1 Hz and holds a local time-series store; two of them beside a server, a
   database and a browser on a shared runner is untested here.
3. **Which of the 47 transport-free tests have a natural home.** The
   classification is by file, and a handful may split across destinations. AT-5
   enumerates them against the code rather than against this list.
4. **The mutant count for `internal/app`.** The carve-out decision waits on it
   (AT-1 step 4); it is not assumed.
5. **Whether `docs/product/` stays twelve chapters.** The gate reads the directory
   rather than a fixed list, so a thirteenth chapter fails until it has an
   outcome — which is the intent, but it does mean a docs commit can turn the
   suite red. The alternative — a hand-maintained chapter list — trades that for
   a list that silently rots. The reading-the-directory form is chosen
   deliberately.
6. **The ADR number.** 081 is the next free one and is also the number
   [`performance-test-strategy-revision.md`](../performance-test-strategy-revision.md)
   reserves. Whichever lands first takes it.

### Overlap with the performance-test plan

That plan touches two of the same things, and both must not land the change:

| Item | Both plans want | Who should own it |
|---|---|---|
| `deploy/docker-compose.test.yml` | expose QUIC, delete the `OPENGATE_TEST_MODE` variable no Go source reads | **this plan** — AT-6 rewrites the file's bring-up anyway; the performance plan builds a *separate* runner-hosted compose file and can drop its D9 clause for this one |
| ADR number 081 | a new decision record | first to land |

Neither overlap is a conflict of substance: one wants a disposable stack that
carries agents for correctness, the other a runner-hosted stack sized for load.
They are two files, and the shared line is one dead variable.
