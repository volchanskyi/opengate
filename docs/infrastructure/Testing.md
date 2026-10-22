# Testing

- [Test databases](#test-databases)
  - [Postgres](#postgres)
  - [Telemetry store](#telemetry-store)
- [Test layers](#test-layers)
  - [Where a test belongs](#where-a-test-belongs)
  - [What a test may assert](#what-a-test-may-assert)
  - [Acceptance tests](#acceptance-tests)
  - [Real machines in the browser stacks](#real-machines-in-the-browser-stacks)
- [Running tests](#running-tests)
  - [Commands](#commands)
  - [Coverage](#coverage)
  - [Mutation testing](#mutation-testing)
  - [Mutation trend](#mutation-trend)
  - [Property tests](#property-tests)
- [End-to-end tests](#end-to-end-tests)
  - [Suites](#suites)
  - [Fixtures](#fixtures)
  - [Shared backend state](#shared-backend-state)
  - [Configuration](#configuration)
  - [Running them locally](#running-them-locally)
  - [Docker credential helpers](#docker-credential-helpers)
- [Security and middleware tests](#security-and-middleware-tests)
  - [Tenancy contracts](#tenancy-contracts)
- [Cross-component tests](#cross-component-tests)
  - [Agent session handler](#agent-session-handler)
  - [Relay frame round trip](#relay-frame-round-trip)
  - [The direct link](#the-direct-link)
  - [Agent update pipeline](#agent-update-pipeline)
  - [Triage, from agent to resolution](#triage-from-agent-to-resolution)
- [When production misbehaves](#when-production-misbehaves)

The project follows a strict test-first approach. All logic is covered before shipping, the Go test runner always uses `-race`, and every `db.Store` test wipes its state before running — no shared state between test cases.

This page is about whether the system gives the right answer. How it behaves under load, over time, against a clock and given hostile input — load and soak runs, leak checks, benchmarks and fuzzing — is [Non-Functional Testing](./Non-Functional-Testing.md).

## Test databases

### Postgres

`db.Store` contract tests run against a real PostgreSQL 17 service container. The shared tests in [`server/internal/db/store_test.go`](../../server/internal/db/store_test.go) use a factory pattern and `storeFactories` table. Integration and handler tests obtain stores through [`server/internal/testutil/testutil.go`](../../server/internal/testutil/testutil.go)'s `NewTestStore(t)`, which creates a fresh PostgreSQL schema (`ogt_<uuid>`) per test, runs migrations on it, and drops it on cleanup — so tests may safely call `t.Parallel()`. Each test pool caps at 3 connections and a process-wide semaphore limits concurrent live stores; with the default Postgres `max_connections=100` the working set can saturate when many parallel tests overlap, so the project's Makefile (`make postgres-test-up`) and CI launch Postgres with `-c max_connections=400`. When `POSTGRES_TEST_URL` is unset, [`server/internal/testpg`](../../server/internal/testpg/testpg.go) starts a throwaway container with the same setting and fails loudly if it cannot — a database-backed test always runs. Every helper that provisions a container settles the reaper through [`server/internal/testreaper`](../../server/internal/testreaper/testreaper.go), so each package process creates and waits on its own container reaper rather than one a sibling process owns — the wait for somebody else's is a fixed minute with no setting behind it, and a busy machine exceeds it.

To run the DB tests locally:

```bash
# Auto-provisions its own Postgres
go test -race -timeout 5m ./internal/db/...

# Or point the suite at one you already have, which is faster across
# several packages and lighter on a memory-tight machine
make postgres-test-up
POSTGRES_TEST_URL="postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable" \
  go test -race -timeout 5m ./internal/db/...
```

### Telemetry store

The packages under [`server/tests/`](../../server/tests) that measure the central
store — cardinality, backfill, and per-series cost — run against a real
VictoriaMetrics, never a mock. [`server/internal/testvm`](../../server/internal/testvm/testvm.go)
supplies it: `VICTORIAMETRICS_TEST_URL` when set, otherwise a throwaway
container, failing loudly rather than skipping. It settles the reaper the same
way `testpg` does, so a package that imports only this one still gets a reaper
of its own.

Share one instance across the run. `testvm` memoizes per test **binary** and
`go test ./tests/...` builds one binary per package, so with the URL unset each
package starts its own store, and several of them holding a fleet's worth of
series at once is enough memory pressure for the kernel to kill one mid-run —
which surfaces as an unrelated package failing on a refused connection.
`make victoriametrics-test-up` starts the shared instance and prints the export;
[`scripts/test-go.sh`](../../scripts/test-go.sh) (behind `make test-go`) and the
precommit gauntlet both do it automatically. Every CI job that runs the tree
provisions it the same way — the integration job in
[`ci.yml`](../../.github/workflows/ci.yml), and the shard-budget and matrix jobs
in [`mutation.yml`](../../.github/workflows/mutation.yml), whose Go shards each
re-run the whole suite as their coverage baseline.
[`scripts/tests/go-test-scope-parity.test.sh`](../../scripts/tests/go-test-scope-parity.test.sh)
holds them to it.

A measurement that reads the store's *own* memory or disk needs the opposite —
an instance nothing else writes to — and takes one through `testvm.Dedicated`,
which ignores the shared URL by design.

## Test layers

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Load Tests                                 │
│  k6 HTTP/WS scenarios + Go QUIC harness (staging, on-demand)        │
├─────────────────────────────────────────────────────────────────────┤
│                         E2E (Playwright)                            │
│  The browser against a stack carrying two real enrolled machines    │
├─────────────────────────────────────────────────────────────────────┤
│                          Acceptance                                 │
│  One outcome per product capability, through the two doors only     │
├─────────────────────────────────────────────────────────────────────┤
│                        Golden (cross-language)                      │
│  Rust generates binary fixtures  →  Go verifies bit-identical       │
├─────────────────────────────────────────────────────────────────────┤
│                        Integration                                  │
│  What needs a transport: real QUIC, real sockets, WebSockets        │
├────────────────────────────────┬────────────────────────────────────┤
│         Unit (Go)              │           Unit (Rust)              │
│  auth, DB, cert, API,          │  protocol codec, platform traits,  │
│  protocol, agentapi, relay     │  agent-core, proptest              │
├────────────────────────────────┴────────────────────────────────────┤
│                     Web Unit / Component                            │
│  React components, Zustand stores, protocol codec, transport,      │
│  session features (desktop, terminal, files, chat) (Vitest + RTL)  │
├─────────────────────────────────────────────────────────────────────┤
│                     Web Integration                                 │
│  Full page flows: auth, routing, device list/detail with stores     │
└─────────────────────────────────────────────────────────────────────┘
```

| Layer | Stack | Location |
|-------|-------|----------|
| **Unit (Go)** | `testing` + testify | `server/internal/*/` |
| **Unit (Rust)** | `#[test]` + proptest | `agent/crates/*/` |
| **Property** | proptest (Rust), rapid (Go), fast-check (Web) | `*property*` test files in all three trees |
| **Fuzz (Rust)** | cargo-fuzz / libFuzzer on nightly Rust, in CI on every push, + stable corpus replay | [`agent/fuzz/`](../../agent/fuzz), [`decode_corpus_test.rs`](../../agent/crates/mesh-protocol/tests/decode_corpus_test.rs) |
| **Fuzz (Go)** | native `go test -fuzz`, seeded from the goldens | [`codec_fuzz_test.go`](../../server/internal/protocol/codec_fuzz_test.go) |
| **Acceptance** | the whole product over [`internal/app`](../../server/internal/app), live Postgres, two actors | `server/tests/acceptance/` |
| **Integration** | real QUIC + real sockets + live Postgres | `server/tests/integration/` |
| **Golden** | Rust generates → Go verifies | `testdata/golden/` |
| **Web Unit** | Vitest + React Testing Library | `web/src/**/*.test.{ts,tsx}` |
| **Web Integration** | Vitest + React Testing Library | `web/tests/integration/**/*.test.tsx` |
| **E2E** | Playwright (Chromium) | `web/e2e/*.spec.ts` |
| **Load (HTTP)** | k6 | `load/k6/scenarios/` |
| **Load (QUIC)** | Go harness | `server/tests/loadtest/` |

### Where a test belongs

Three Go tiers, and one question decides which a test goes in: *what does it
need in order to run at all?*

| Tier | Holds | Address |
|---|---|---|
| **Unit** | one package's behaviour, including its HTTP surface | `server/internal/<pkg>/` |
| **Integration** | what needs a transport — a real QUIC peer, a real socket, a WebSocket, two servers | [`server/tests/integration/`](../../server/tests/integration) |
| **Acceptance** | one outcome per product capability, in the words a customer would use | [`server/tests/acceptance/`](../../server/tests/acceptance) |

The rule is enforced rather than remembered:
[`test-tier-placement.test.sh`](../../scripts/tests/test-tier-placement.test.sh)
refuses a test in the integration tier that reaches no transport, refuses an
acceptance test that imports a repository package outside the harness's
arrangement helpers, and holds every acceptance test to `t.Parallel()`.

### What a test may assert

Where a test lives says what it needs. What it may assert is decided separately,
by [`.claude/rules/test-value.md`](../../.claude/rules/test-value.md), and rests
on one line: **a test asserts on the code that ships, and leaves the environment
as it found it.**

Four things follow from it, and two of them are machine-checked:

| | Held by |
|---|---|
| A test exercises the shipped module rather than a copy pasted into the test file | [`test-value-check.sh`](../../scripts/test-value-check.sh) — a test beside `foo.ts` binds `foo`'s own export |
| A test that patches a global or a prototype restores it | the same analyser — an assignment inside a `finally`, `afterEach` or `afterAll` puts the original back |
| A test asserts behaviour the product has, not a literal it just built, a compile-time constraint, or a third-party library | review |
| A module awkward to test is reached by a different test approach, never by adding an export or a factory that exists only for the test | review |

Both machine checks run twice: at write time through
[`pretooluse-test-value-guard.sh`](../../.claude/hooks/pretooluse-test-value-guard.sh),
and over every tracked web test in the gauntlet through
[`test-value.test.sh`](../../scripts/tests/test-value.test.sh).

**What is deliberately not checked matters as much.** Grading tests by the shape
of their assertions and deleting the weak-looking ones inverts the correlation
it assumes: measured against the nightly breakage report, the web test files
with the most "weak" assertions catch *more* breakage than the files with none.
So presence-only queries that pin a literal string, styling assertions where
colour is the only carrier of state, and DOM walks such as `closest('li')` or a
`querySelector('script')` XSS check are all legitimate, and the rule file names
each one and why. A change that lowers the caught-rate is not a cleanup.

### Acceptance tests

An acceptance test stands the whole product up — the composition root in
[`internal/app`](../../server/internal/app), a real database, an HTTP listener
and a QUIC listener — and then speaks through exactly two doors, because a real
installation has exactly two:

| Actor | Door |
|---|---|
| **Technician** | the HTTP API, issuing the requests the browser issues |
| **Machine** | the QUIC control stream, speaking the wire the golden fixtures prove the Rust agent identical to |

Everything is real except the three edges that sit outside the product: Intel
management hardware, which answers on its own network path; browser push
delivery; and the GitHub release feed. Where the product offers an operator no
door to create a precondition, the harness seeds it, and the helper is named
`arrange…` so the exception is visible in the test's own text.

Every chapter of [`docs/product/`](../product) is bound to the outcomes that
prove it, and the binding is checked in both directions out of the package's own
syntax tree: a capability with no outcome fails the suite naming the chapter, and
an outcome naming no capability fails it the other way. Adding a chapter turns
the suite red until somebody states what a customer gets from it, which is the
intent.

### Real machines in the browser stacks

The suite runs against two stacks — the local one and the deployed staging
release — and both bring up the machines the specs name.
[`enrolled-machine.ts`](../../web/e2e/helpers/enrolled-machine.ts) pins those
names in one place: `agent-a` is what the device pages are read against,
`agent-b` is the expendable one, for a spec that wants to disturb a machine.
The local stack carries two more, `agent-c` and `agent-c-copy`, which only
[`machine-lifecycle.spec.ts`](../../web/e2e/machine-lifecycle.spec.ts) starts:
`agent-c` keeps its data folder on a named volume, so its identity survives a
stop and a start, and `agent-c-copy` starts from a copy of that folder, as a
cloned disk would. The spec deletes a machine while it is off, reinstalls over a
deleted machine, repairs a live one, cuts the power during a fresh reinstall,
reinstalls over a half-finished removal, and runs two copies of one identity,
reading each container's log, data folder and exit beside the console.

[`docker-compose.test.yml`](../../deploy/docker-compose.test.yml) runs the local
stack — a database, a metrics store, a server listening for machines, and the
agents with pinned hostnames — on three networks: an "internet" one, and an office and a home
network each behind a router container
([`router.sh`](../../deploy/router/router.sh)) that translates addresses, limits
the rate and drops UDP when a spec tells it to. `agent-a` sits behind the office
router and `agent-b` on the internet network beside the server. A Playwright
browser server sits behind the home router, so a spec can put both sides of a
session behind routers, the usual case in the field. Only the two routers hold
network-admin rights, held by
[`compose.rego`](../../policy/docker_compose/compose.rego);
[`e2e-stack-machines.test.sh`](../../scripts/tests/e2e-stack-machines.test.sh)
holds where each container sits, each machine on one CPU — a sender moving
between CPUs reaches the far side out of order, which the direct link reads as
loss — and the console published on IPv4 alone, since Firefox offers no
direct-link address on a page it reached over IPv6 loopback. The networks need
Docker Engine 28, which the bring-up checks first.

The machines install the way a real one does.
[`e2e-stack-up.sh`](../../deploy/scripts/e2e-stack-up.sh) builds every image once,
from one reading of the tree — the server keeps its master key in memory, so a
second build would start it under a key the machines' note does not name — then
starts the database, the metrics store and the server, signs in as the bootstrap
operator, mints an enrolment token through the public endpoint, and writes the
install note the installer would
([`install-note.sh`](../../deploy/scripts/install-note.sh)). Each agent's start
step ([`agent-start.sh`](../../deploy/agent-start.sh)) copies that note into its
data folder owner-only, as the installer does; each agent then generates its own
key, asks to be signed, and connects. The metrics store is there because erasing
a device erases its series too, and a server with no store refuses the delete. No private key is copied and no test-only
affordance exists in the shipped server. `make e2e` and Playwright's `webServer`
both call that script, so the two paths cannot stand up different stacks.

On staging the same two machines are pods, created by the deploy job in
[`cd.yml`](../../.github/workflows/cd.yml) from
[`e2e-machine-pod.sh`](../../deploy/scripts/e2e-machine-pod.sh) — a pod's
hostname is its name, which is what the specs look a machine up by. They run in
the namespace rather than on the runner because an agent speaks QUIC over UDP
and `kubectl port-forward` carries TCP only, the conclusion the load run reached
for its own fleet. Their binary is cross-built in that job for the node's
architecture, from the commit being deployed, so the machines and the server
they enrol into are always the same version of the product. They enrol through
the same public endpoint, with a token minted for the run against the bootstrap
operator, and both they and the token are removed whatever the suite's verdict.

Values differ per runner — the processor, the memory, the addresses — so specs
assert on shape and presence, reading what the machine reported out of the
response and checking the page carries it.
[`e2e-stack-machines.test.sh`](../../scripts/tests/e2e-stack-machines.test.sh)
holds both stacks to the names that one file pins, checks that the name a
staging machine dials is the name the chart puts on the machine-facing
certificate, and refuses a spec that goes back to inventing a machine.

## Running tests

```bash
make test               # All tests — Rust + Go + Web
make test-go            # Go server (unit + integration) with race detector
make test-integration   # Integration suite only
make agent-binary       # The static agent binary the browser stack's machines run
make test-rust          # Rust workspace
make test-web           # React / TypeScript
make test-coverage      # Go coverage report printed to stdout
make golden             # Regenerate golden fixtures and verify cross-language compat
make e2e                # Playwright E2E against a stack carrying two real machines
make load-test          # All three k6 scenarios against localhost:8080
make load-test-quic     # Go QUIC load harness (100 concurrent machines)
```

### Commands

```bash
# Go — unit + integration, race detector, 5 min timeout
cd server && go test -race -timeout 5m ./...

# Rust — all crates
cd agent && cargo test --workspace

# Web
cd web && npx vitest run
```

### Coverage

All three languages enforce a minimum line-coverage threshold both in CI and locally (via the [precommit gauntlet](../../scripts/precommit-gauntlet.sh)). The enforced values live in the coverage steps of [`ci.yml`](../../.github/workflows/ci.yml) (search for `THRESHOLD` and `fail-under-lines`) — the commands below mirror them:

```bash
# Go — coverage; only test scaffolding and generated code are filtered out
cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
grep -v -E '/testutil/|api/openapi_gen\.go' coverage.out > coverage-prod.out
go tool cover -func=coverage-prod.out | grep total

# Web — Vitest v8 coverage, check summary JSON
cd web && npx vitest run --coverage
node -e "const s=require('./coverage/coverage-summary.json');const l=s.total.lines.pct;console.log('Web line coverage: '+l+'%');process.exit(l<80?1:0)"

# Rust — cargo-llvm-cov; only the test files themselves are ignored
cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
  --ignore-filename-regex '(/tests/)'
```

Every production path counts toward the threshold in all three languages. A
coverage exclusion is a last resort that needs explicit approval, names the
reason no in-process test can execute the file, and is deleted as soon as that
stops being true — see
[`.claude/rules/coverage-exclusions.md`](../../.claude/rules/coverage-exclusions.md).
[`scripts/sonar-coverage-exclusion-guard.sh`](../../scripts/sonar-coverage-exclusion-guard.sh)
enforces the mechanical half in the gauntlet: every entry justified, no listed
path missing, the per-language ignore lists identical across CI and the local
run, and a file split out of an excluded file carrying its exclusion with it.

The list holds no production file. What remains is test scaffolding, generated
output, and the two browser entry points, which is where it is meant to end.

### Mutation testing

Coverage % asserts which lines executed; mutation score asserts which lines
were *meaningfully* tested. Run `make mutate` to drive cargo-mutants (Rust),
gremlins (Go), and stryker (Web).

**Carve-outs** (genuinely unmutateable code, analogous to platform shims). Each
config carries a per-entry justification next to the exclusion it explains:
- Rust: [agent/.cargo/mutants.toml](../../agent/.cargo/mutants.toml) — platform
  shims, agent binary entry point, SELinux restorecon match guards, and the nine
  `edge-tsdb` bake-off substrates. Those last are a different kind of carve-out:
  not code without a harness, but code no customer runs. They sit behind a
  feature the shipped agent turns off, and cargo-mutants builds with default
  features, so without the carve-out a fifth of the Rust leg's unnoticed
  breakages were in a reference implementation. What keeps that true is
  [`noship_test.rs`](../../agent/crates/mesh-agent/tests/noship_test.rs), which
  fails if any crate in the workspace turns the feature back on.
- Go:   [server/.gremlins.yaml](../../server/.gremlins.yaml) — `openapi_gen.go`,
  `cmd/meshserver/main.go`, `tests/loadtest/main.go`, `internal/testutil/`.
- Web:  [web/stryker.config.json](../../web/stryker.config.json) — `main.tsx`,
  `router.tsx`, `icons.tsx`, generated `*.d.ts`, and the two hooks whose logic
  only runs against a real DOM (`use-terminal.ts`, `use-remote-desktop.ts`).

Rust runs need `OPENGATE_GOLDEN_DIR=<repo>/testdata/golden` so golden file
tests resolve fixtures inside cargo-mutants' temp tree. The `mutate-rust`
make target sets this automatically.

CI shard ids and source ownership for both languages live in
[`mutation-shards.sh`](../../scripts/lib/mutation-shards.sh). The behavioral guard in
[`mutation-workflow.test.sh`](../../scripts/tests/mutation-workflow.test.sh) requires
every non-test source to belong to one mutation unit or an explicit carve-out,
so shard reports can be merged without duplicate source counts.

A Go shard is pointed at the narrowest path that still holds its own units, and
that path is derived from the shard map rather than listed beside it. It matters
because gremlins uses the path twice — it walks it for mutants, and it runs `go
test` over it to survey which lines are covered — so a shard aimed at the module
root drags `tests/acceptance`, `tests/integration`, `tests/loadtest`,
`tests/netfault` and `tests/vmramseries` through its survey before mutating a
line of its own. The narrowing is free in mutants: coverage is per-package, so a
harness package's tests never contributed a line of `internal/` coverage, and the
two invocations produce mutant sets over `internal/` that match file by file,
line by line and status by status. The shard owning `tests/loadtest` and
`tests/netfault` keeps the module root, because a walk that cannot see a file
cannot mutate it. See [ADR-122](../adr/ADR-122-mutation-walk-and-legs.md).

Each shard is named for the behavior it mutates, so a red leg says what lost
coverage rather than which slice of an interleaved list failed. How many mutants
a shard may hold is a measurement, not a habit, and what one mutant costs differs
by an order of magnitude on both legs:

| Leg | What a mutant pays for | Recorded in |
|---|---|---|
| Rust | The per-mutant rebuild of its cargo package — `mesh-agent-core` relinks twenty-six test binaries, `edge-tsdb` far fewer | `mutation_rust_package_milliminutes_per_mutant` |
| Go | Re-running the test packages that cover the mutated line — an `internal/api` handler re-pays the Postgres-backed API suite and the integration tests that reach it, an `internal/agentapi` mutant an in-process harness | `mutation_go_shard_seconds_per_mutant` |
| Web | Re-running the Vitest files that cover the mutated line, which differs by feature folder — the device pages, the session tab and the rest of the console | `mutation_web_shard_milliseconds_per_mutant` |

Both numbers come from completed nightly shards and are re-measured from a run
rather than lowered to make a shard fit.

The projection is checked before the matrix runs by
[`mutation-shard-budget.sh`](../../scripts/mutation-shard-budget.sh) — a shard that
has outgrown the job cap is reported in a few minutes instead of taking the whole
nightly down with it after ninety. The Go count comes from a `gremlins` dry-run
rather than from the source, because a Go mutant runs only where coverage reaches
it: adding an integration test grows a shard without a line of production code
changing. The web count comes from Stryker itself, instrumenting each shard's
files with a command runner that runs no test
([`mutation-web-count.sh`](../../scripts/mutation-web-count.sh)), so a console
grown past a shard's budget is reported by that check rather than by a shard cut
off at the cap.
The web shards' reports are joined into one by
[`mutation-merge-web.sh`](../../scripts/mutation-merge-web.sh), which refuses two
shards reporting one file.

A shard's wall clock has a second term the count does not reach.
`CONDITIONALS_NEGATION` on a loop's exit condition produces a mutant that never
terminates — a batched delete that repeats after reclaiming nothing, a key-padding
loop that prepends zero bytes without end — and gremlins gives every mutant the
coverage run's elapsed time multiplied by
[`timeout-coefficient`](../../server/.gremlins.yaml) before cutting it off. One
such mutant holds a worker for all of it, so a shard of cheap mutants can still
fill most of a job.

Both terms are declared: `mutation_go_shard_blocking_mutants` counts the mutants
that never terminate, and the projection adds a full leash for each. The
coefficient is bounded so that leash stays inside what a fully-spent shard has
left of the ninety minutes, which is what keeps one of them from taking the job
past the cap on its own.

### Mutation trend

Mutation tests do **not** gate merges or deploys. They run **nightly** via the
[mutation.yml workflow](../../.github/workflows/mutation.yml) and
emit a row per run to:

- **VictoriaMetrics** — mapped by
  [`scripts/mutation-vm-push.sh`](../../scripts/mutation-vm-push.sh) for complete
  score rows and
  [`scripts/mutation-status-vm-push.sh`](../../scripts/mutation-status-vm-push.sh)
  for run/shard completeness, both sent through the shared
  [`vm-push.sh`](../../scripts/lib/vm-push.sh) transport. Visualised by the provisioned
  [`mutation-trend.json`](../../deploy/grafana/provisioning/dashboards/mutation-trend.json)
  dashboard. Canonical trend store per
  [ADR-038](../adr/ADR-038-ci-trend-store.md).
- **Workflow artifacts** — every run uploads `mutation-run-status`, which carries
  completeness for the run as a whole and for each language. A run with at least
  one complete leg uploads `mutation-canonical-row`. Validation and the
  no-partial-leg contract are implemented by
  [`mutation-status-build.sh`](../../scripts/mutation-status-build.sh) and the strict
  language merge scripts beside it.

Numeric mutation-score history lives in VictoriaMetrics + Grafana, the right
home for time-series telemetry.

A leg is published when all of its own shards finished, and a leg short a shard
emits no score at all — a score over the shards that happened to finish is a
smaller number nothing marks as partial. A night where one leg is short still
publishes the others and still checks them for regressions, and still goes red:
the gate reads the whole run. See [ADR-122](../adr/ADR-122-mutation-walk-and-legs.md).

**Regression alert rules** — fired when any language regresses on either
condition: its absolute score crosses below the floor, or it drops by more than
the allowed margin from the previous successful run. Both values are the
`REGRESSION_FLOOR_PCT` / `REGRESSION_DROP_PP` constants in
[`mutation-summarize.sh`](../../scripts/mutation-summarize.sh).

The `no_coverage` field is reported as `—` for Rust: cargo-mutants does not
distinguish "missed" from "not covered" — every untested mutant lands in
`missed` / `Survived`. The field is preserved in the canonical row for shape
consistency across languages but encoded as `null`.

On regression the workflow goes red ❌ in the GitHub Actions history and
sends a Telegram alert via the existing `DEPLOY_TELEGRAM_BOT_TOKEN` /
`DEPLOY_TELEGRAM_CHAT_ID` secrets. Nothing else blocks; `merge-to-main`
remains independent.

Mutation testing runs as a **non-blocking observability signal**, not a merge
gate: a survived-mutant regression turns the workflow red and alerts, but never
blocks `merge-to-main`.

### Property tests

A table-driven test samples an invariant; a property test states it and lets a
generator attack it. Each language uses the idiomatic generator library for its
stack — [`proptest`](../../agent/Cargo.toml) for Rust,
[`pgregory.net/rapid`](../../server/go.mod) for Go, and
[`fast-check`](../../web/package.json) for TypeScript — and all of them run
inside the ordinary unit-test commands above. There is no separate target and no
nightly job: a shrunk counterexample fails the gauntlet like any other assertion,
and the shrinker reports the minimal input rather than the random one that
tripped it.

The wire protocol carries most of the weight, in two shapes:

- **Round-trip identity** — `decode(encode(x)) == x` over generated messages.
  [`property_test.rs`](../../agent/crates/mesh-protocol/tests/property_test.rs)
  builds `arb_control_message()` by composing strategies for session tokens,
  capabilities and permissions;
  [`codec_property_test.go`](../../server/internal/protocol/codec_property_test.go)
  covers frames, ping/pong, server-hello and handshake types; and
  [`codec.property.test.ts`](../../web/src/lib/protocol/codec.property.test.ts)
  adds two properties a fixture rarely reaches — the decoder consumes exactly the
  bytes the encoder produced, and it reads correctly from a non-zero byte offset
  inside a larger buffer.
- **Controlled failure on arbitrary bytes** — every decoder returns an error for
  any input a hostile peer can put on the wire. This is the same contract the
  fuzz targets below explore with coverage guidance; the property test is its
  always-on floor.

Away from the codec, the properties state domain rules directly:

- [`gorilla.rs`](../../agent/crates/edge-tsdb/src/gorilla.rs) round-trips
  arbitrary timestamp/value series through a compression block, comparing floats
  by bit pattern so NaN payloads are asserted rather than silently excused.
- [`apf_property_test.go`](../../server/internal/amt/transport/apf_property_test.go)
  round-trips the AMT/APF message types and asserts `ReorderIntelGUID` returns a
  permutation of its input.
- [`converters_property_test.go`](../../server/internal/api/converters_property_test.go)
  pins order, length, pointer identity and pagination across the API converters.
- [`token-status.property.test.ts`](../../web/src/lib/token-status.property.test.ts)
  states the token rules as algebra: an unparseable expiry counts as expired
  (fail-safe), and a token is active exactly when it is neither expired nor
  exhausted.
- [`file-store.property.test.ts`](../../web/src/features/file-manager/state/file-store.property.test.ts)
  is model-based — it drives arbitrary sequences of store actions against a
  reference model and asserts the two stay in agreement.

Property tests pair naturally with the mutation runs above: when a mutant
survives inside a codec or a converter, a sharper property usually kills it more
durably than another example row.

## End-to-end tests

E2E tests run Playwright against a real server instance via [`docker-compose.test.yml`](../../deploy/docker-compose.test.yml): a Postgres container, a server container built from source, and the two machines described above. The database and the server back their state with tmpfs, so teardown is instant.

### Suites

One spec per capability, in [`web/e2e/`](../../web/e2e/). Each names in its own
header what a technician does in it, so the directory listing is the index.

### Fixtures

Custom Playwright fixtures in `web/e2e/fixtures.ts` provide:
- `testUser` — registers a fresh user via API before each test
- `authedPage` — a page signed in through the console's session endpoint, so
  the browser context holds the session cookie, as a technician's would

### Shared backend state

Every spec runs against one backend, and every e2e user registers into the same
tenant — which is the visibility boundary for groups and devices. A group
one spec creates is therefore visible to every spec that runs after it, so a
spec that asserts an empty fleet is really asserting on what the whole suite has
done so far. Two rules follow:

- A spec that seeds real fleet state deletes it again, pass or fail. The
  `globalTeardown` in `web/e2e/global-teardown.ts` fails the run if anything is
  left behind, so a leak is attributed to the run that caused it rather than to
  whichever later spec goes red.
- A spec that asserts an *empty* UI state supplies that emptiness itself, via
  `stubEmptyFleet` in `web/e2e/helpers/fleet-stub.ts`, instead of reading the
  shared backend.

The same applies to Administrators membership: a spec that empties the group to
reach a "last admin" state restores it, or the bootstrap admin that global setup
depends on is gone for every later run against that database.

### Configuration

- **CI**: `web/playwright.config.ts` — targets `http://localhost:8080` (docker-compose)
  from Chromium on the test machine, and the two-router spec from a Chromium on
  the browser server behind the home router. Nightly, Firefox and WebKit run the
  same way. The speed specs run apart from both: under `PLAYWRIGHT_PERFORMANCE=1`
  the config holds one Chromium project of them alone, which
  [`e2e-performance.yml`](../../.github/workflows/e2e-performance.yml) runs
  nightly, and every other run leaves them out
  ([`e2e-speed-nightly.test.sh`](../../scripts/tests/e2e-speed-nightly.test.sh)).
  Both nightly runs write their summary from the JSON report
  ([`e2e-summarize.sh`](../../scripts/e2e-summarize.sh)).
- **Staging**: `web/playwright.staging.config.ts` — derives from the CI config and
  overrides only the target, the retry policy and the specs that drive the local
  stack, so the two cannot drift in how the suite executes;
  `scripts/tests/playwright-config-parity.test.sh` enforces it. Staging has no
  router, no machine shell and no browser server, so the direct-link, download,
  performance and two-router specs and the projects behind the home router stay
  local: staging proves the relay, and the terminal echo there is real. What it
  does not inherit is the stack: the deploy job stands staging's machines up,
  which is why they are checked against the same pinned names

### Running them locally

```bash
# Build the agent binary, bring the stack up, run Playwright, tear down
make e2e

# Or step by step. The bring-up is a script rather than a `compose up` because
# a machine needs an enrolment token and a token can only be minted once the
# server answers, so the stack comes up in two halves with a mint between them.
make agent-binary
cd deploy && bash scripts/e2e-stack-up.sh
cd ../web && npx playwright test
cd ../deploy && docker compose -f docker-compose.test.yml down -v

# The speed specs alone, as the nightly runs them
PLAYWRIGHT_PERFORMANCE=1 make e2e
```

### Docker credential helpers

Use `make e2e` rather than a bare `docker compose` — the target owns the full
up/build/down lifecycle and routes docker through a sanitized credential config.

A `~/.docker/config.json` with a `"credsStore"` pointing at a helper that cannot
execute makes `docker build`/`pull` fail **even for public base images**
(`alpine`, `node`, `golang`), because docker invokes the helper before falling
back to anonymous access. On WSL the usual offender is
`docker-credential-desktop.exe` failing with `exec format error` when Docker
Desktop's WSL integration is not wired up:

```
ERROR: error getting credentials - err: fork/exec
/usr/bin/docker-credential-desktop.exe: exec format error
```

[`scripts/docker-credstore-guard.sh`](../../scripts/docker-credstore-guard.sh)
handles this automatically: it probes the configured helper and, only if it is
broken, exports a `DOCKER_CONFIG` whose `config.json` has `credsStore`/
`credHelpers` stripped (every other key — including `auths` — preserved), so
public images pull anonymously. It is wired into `make e2e` and the precommit
gauntlet. It is a **no-op** when the helper works or none is configured, so CI
(where docker login writes `auths` directly, with no broken `credsStore`) is
unaffected. To run any ad-hoc docker command through the same guard:

```bash
DOCKER_CONFIG="$(scripts/docker-credstore-guard.sh)" docker compose ...
```

If you prefer a permanent local fix instead, remove the dead `credsStore` line
from `~/.docker/config.json` (Docker then pulls public images anonymously and
still honours any `auths` entries).

## Security and middleware tests

The codebase audit added targeted tests for security hardening:

| Test File | Coverage |
|-----------|----------|
| `server/internal/api/ratelimit_test.go` | Under/over limit behavior, per-IP independence, `X-Forwarded-For` parsing |
| `server/internal/api/middleware_test.go` | `RequestTimeout` middleware, HSTS header assertion in `SecurityHeaders` |
| [`server/internal/api/auth_handlers_test.go`](../../server/internal/api/auth_handlers_test.go) | Email validation (invalid formats rejected, valid formats accepted) |
| `web/src/components/ErrorBoundary.test.tsx` | Error boundary renders fallback UI on child component crash |
| `server/tests/integration/middleware_ws_test.go` | Full middleware stack preserves `http.Hijacker` for WS upgrades, relay route bypasses 30s `RequestTimeout` |

The Playwright E2E suite in [`web/e2e/`](../../web/e2e) passes with the auth rate limiter active, confirming no regressions from the middleware.

### Tenancy contracts

Two tests enforce the tenancy rules across the whole surface rather than one
endpoint at a time, so a table or an endpoint added later cannot quietly opt out:

| Test | What it enforces |
|---|---|
| [`TestTenantIsolationCoversEveryTenantTable`](../../server/internal/db/tenant_isolation_test.go) | For every tenant table: the caller sees its own row and not the other tenant's, in both directions, and an unscoped read fails closed rather than reading as an empty tenant |
| [`TestEveryTenantTableIsProbed`](../../server/internal/db/tenant_isolation_test.go) | That the contract above covers the whole schema. The probes are hand-written static SQL, so this reads back the tables carrying `tenant_id` and insists the two lists agree — a table added later with no probe fails here instead of going unproven |
| [`TestFleetReadsOfferTheOrganizationFilter`](../../server/internal/api/organization_filter_contract_test.go) | Every operation in [`api/openapi.yaml`](../../api/openapi.yaml) whose 200 response is a set of devices, or a rollup over one, declares the `organization_id` query parameter. Derived from the response shape, so a fleet read added without the filter fails rather than showing a technician every customer at once |

## Cross-component tests

These tests exercise multi-component interaction paths that unit tests cannot cover:

### Agent session handler

[`agent/crates/mesh-agent-core/src/session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs) covers frame dispatch, permission enforcement, and error paths:

| Test | What It Verifies |
|------|-----------------|
| `a_ping_is_answered_with_a_pong` | Ping frame → Pong response |
| `terminal_bytes_reach_the_shell_and_are_dropped_without_one` | Terminal bytes reach an open shell, and are dropped with none open |
| `a_screen_picture_from_the_browser_is_ignored` | A desktop frame from the browser is ignored |
| `input_is_injected_only_with_the_input_permission` | `permissions.input` decides whether the `InputInjector` is called |
| `a_key_press_also_reaches_an_open_terminal_and_a_resize_its_size` | Key presses and resizes reach an open terminal |
| `a_chat_message_is_echoed_as_the_agent` | `ChatMessage` → echoed with `sender: "agent"` |
| `a_chat_message_too_large_to_echo_is_refused_and_the_session_carries_on` | An echo over the interactive lane's cap is refused, and the session carries on |
| `file_requests_reach_the_file_handler` | File requests reach the file handler |
| `an_undecodable_frame_is_dropped` | An undecodable frame is dropped |

Uses `RecordingInjector` mock that records all `inject_*` calls for assertion.

### Relay frame round trip

`server/tests/integration/relay_data_test.go` — `TestRelayProtocolFrameRoundTrip` with 4 sub-tests:

| Sub-test | Direction | Frame |
|----------|-----------|-------|
| `control_mouse_move` | browser → agent | msgpack `MouseMove{X:100,Y:200}` |
| `control_file_list_request` | browser → agent | msgpack `FileListRequest{Path:"/home"}` |
| `terminal_frame` | agent → browser | msgpack `TerminalFrame{Data:"ls -la\n"}` |
| `bidirectional_control` | both ways | Simultaneous `MouseMove` + `FileListResponse` |

Sends properly encoded `[type][4-byte BE length][payload]` frames through the full QUIC+WebSocket relay path and verifies msgpack decoding on the receiving side.

### The direct link

The hand-off is proved where it is written, then on real peers, then in browsers
([ADR-127](../adr/ADR-127-direct-link.md)):

| Where | What it proves |
|---|---|
| [`testdata/direct-link/`](../../testdata/direct-link/) | Scripted situations — a drop before the switch, mid-stream in either direction, between a frame's pieces, both sides falling back at once, an interactive frame ahead of queued bulk — with the exact deliveries on both sides. [`direct_link_scenarios_test.rs`](../../agent/crates/mesh-protocol/tests/direct_link_scenarios_test.rs) and [`handoff.test.ts`](../../web/src/lib/direct/handoff.test.ts) replay every one, and each adds 10 000 random drop points |
| [`direct_link_test.rs`](../../agent/crates/mesh-agent-core/tests/direct_link_test.rs) | A browser-side peer on loopback against the host: the session moves onto the link, a silent peer is noticed within the silence limit, and sockets and tasks per completed session stay flat |
| [`download_memory_test.rs`](../../agent/crates/mesh-agent-core/tests/download_memory_test.rs) | The host's memory stays flat against file size during a download |
| [`session_relay_stub_test.rs`](../../agent/crates/mesh-agent-core/tests/session_relay_stub_test.rs) | A second offer refused, a second download refused while one runs, a large folder in pages ahead of nothing |
| [`signaling_relay_test.go`](../../server/tests/integration/signaling_relay_test.go) | The negotiation — `DirectOffer`, `DirectAnswer`, `IceCandidate`, `DirectSwitch` — crosses the relay byte for byte, and an offer nobody answers leaves the session relayed |
| [`helper_test.go`](../../server/tests/integration/helper_test.go) | The helper answers a whole burst of session starts beside connected machines, holds a flood from one address and from many to their limits, and the agents' listener never offers to let peers clear the QUIC fixed bit |
| `link_pieces` in [`agent/fuzz/`](../../agent/fuzz/fuzz_targets/) | Hostile pieces and candidate lines never panic the reassembler or the candidate filter |
| The direct-link, two-router, download, session-tab and performance specs in [`web/e2e/`](../../web/e2e/) | Real browsers on the compose stack: Direct to each machine and across both routers, Through server where the routers block it, inside the attempt budget; drops, downloads, typing during a download, and memory |

### Agent update pipeline

**Go** (the `update*_test.go` files under [`server/tests/integration/`](../../server/tests/integration)):

| Test | What It Verifies |
|------|-----------------|
| `TestUpdatePublishAndPush` | Admin publishes manifest → pushes update → agent receives `AgentUpdate` on QUIC → sends `AgentUpdateAck` → DB records update |
| `TestUpdatePushSkipsCurrentVersion` | Agent already on target version → `pushed_count=0` |
| `TestUpdatePushNoMatchingOS` | Manifest for windows/amd64, agent is linux/amd64 → not pushed |

**Rust** ([`agent/crates/mesh-agent-core/src/update.rs`](../../agent/crates/mesh-agent-core/src/update.rs)):

| Test | What It Verifies |
|------|-----------------|
| `test_apply_update_full_pipeline` | Mock HTTP server serves fake binary → `apply_update()` downloads, verifies SHA-256, validates Ed25519 signature, backs up old binary to `.prev`, replaces current binary, writes `.update-pending` sentinel |

### Triage, from agent to resolution

[`investigations_test.go`](../../server/tests/acceptance/investigations_test.go)
walks one event from the machine that raised it to the technician who closed it:
a machine encodes an `AgentAlert` with compressed evidence onto its own QUIC
control stream, the server admits and files it, the fold opens a room for it, and
the technician's whole half goes through the API the browser uses — reading the
room, reading the evidence out of it, taking it, being refused a resolution that
names no cause code, and closing it with one.

| Test | What It Verifies |
|------|-----------------|
| `TestAnAlertBecomesAnIncidentATechnicianClosesWithACause` | QUIC `AgentAlert` + evidence → stored alert → the room the triage queue hands a technician → evidence decoded from the room → resolution refused over HTTP without a cause code, then accepted with one |

Each leg has unit coverage of its own — admission in
[`internal/agentapi`](../../server/internal/agentapi), folding and lifecycle in
[`internal/alerts`](../../server/internal/alerts), the workspace in
[`investigations.spec.ts`](../../web/e2e/investigations.spec.ts). This is the test
that the legs join up, and it runs against a real Postgres and a real QUIC
listener because the seams it is about only exist there. It lives here rather
than in the Playwright suite because the browser stack runs a server and a
database and no agent, so no spec in it can begin at a machine deciding
something is wrong.

## When production misbehaves

When a test passes in CI but something misbehaves in staging/production, reach for the `/observe` Claude Code skill (`.claude/skills/observe/SKILL.md`) or the same kubectl/Loki paths documented in [Monitoring](./Monitoring.md#investigating-by-hand). The current stack is Kubernetes-native, so starting signals come from pod health, Service port-forwards, and Loki labels rather than Docker container state:

| Playbook | Starting signals |
|----------|-----------------|
| Agent offline | Agent service status on the device, QUIC reachability, Loki `enroll`/`agent` entries |
| Requests slow | p95/p99 latency, slowest routes, DB latency by operation, node CPU/memory |
| Deployment health | Deployment rollout status, `/api/v1/health`, `agents_connected`, ingress 5xx |
| Post-deploy verification | new errors since deploy timestamp, server image tag from the Kubernetes Deployment |

See [Monitoring](./Monitoring.md) for the underlying PromQL/LogQL query patterns.
