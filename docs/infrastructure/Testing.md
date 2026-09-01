# Testing

The project follows a strict test-first approach. All logic is covered before shipping, the Go test runner always uses `-race`, and every `db.Store` test wipes its state before running — no shared state between test cases.

### Database Tests

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

### Telemetry-store tests

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

## Test Layers

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
| **Fuzz (Rust)** | cargo-fuzz / libFuzzer (nightly) + stable corpus replay | [`agent/fuzz/`](../../agent/fuzz), [`decode_corpus_test.rs`](../../agent/crates/mesh-protocol/tests/decode_corpus_test.rs) |
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

### The acceptance tier

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

### Both browser stacks carry real machines

The suite runs against two stacks — the local one and the deployed staging
release — and both bring up the machines the specs name.
[`enrolled-machine.ts`](../../web/e2e/helpers/enrolled-machine.ts) pins those
names in one place: `agent-a` is what the device pages are read against,
`agent-b` is the expendable one, for a spec that wants to disturb a machine.

[`docker-compose.test.yml`](../../deploy/docker-compose.test.yml) runs the local
stack — a database, a server listening for machines, and two agents with pinned
hostnames.

The machines install the way a real one does.
[`e2e-stack-up.sh`](../../deploy/scripts/e2e-stack-up.sh) starts the database and
the server, signs in as the bootstrap operator, mints an enrolment token through
the public endpoint, and starts both agents with it; each generates its own key,
asks to be signed, and connects. No private key is copied and no test-only
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

## Running Tests Locally

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

### Individual commands

```bash
# Go — unit + integration, race detector, 5 min timeout
cd server && go test -race -timeout 5m ./...

# Rust — all crates
cd agent && cargo test --workspace

# Web
cd web && npx vitest run
```

### Coverage enforcement

All three languages enforce a minimum line-coverage threshold both in CI and locally (via `/precommit`). The enforced values live in the coverage steps of [`ci.yml`](../../.github/workflows/ci.yml) (search for `THRESHOLD` and `fail-under-lines`) — the commands below mirror them:

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
  shims, agent binary entry point, SELinux restorecon match guards.
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

Each shard is named for the behavior it mutates, so a red leg says what lost
coverage rather than which slice of an interleaved list failed. How many mutants
a shard may hold is a measurement, not a habit, and what one mutant costs differs
by an order of magnitude on both legs:

| Leg | What a mutant pays for | Recorded in |
|---|---|---|
| Rust | The per-mutant rebuild of its cargo package — `mesh-agent-core` relinks twenty-six test binaries, `edge-tsdb` far fewer | `mutation_rust_package_milliminutes_per_mutant` |
| Go | Re-running the test packages that cover the mutated line — an `internal/api` handler re-pays the Postgres-backed API suite and the integration tests that reach it, an `internal/agentapi` mutant an in-process harness | `mutation_go_shard_seconds_per_mutant` |

Both numbers come from completed nightly shards and are re-measured from a run
rather than lowered to make a shard fit.

The projection is checked before the matrix runs by
[`mutation-shard-budget.sh`](../../scripts/mutation-shard-budget.sh) — a shard that
has outgrown the job cap is reported in a few minutes instead of taking the whole
nightly down with it after ninety. The Go count comes from a `gremlins` dry-run
rather than from the source, because a Go mutant runs only where coverage reaches
it: adding an integration test grows a shard without a line of production code
changing.

A shard whose runtime is dominated by mutants that block rather than by mutants
that run is sized by its timeout instead, through
`mutation_go_shard_timeout_coefficient`: gremlins derives each mutant's budget
from the coverage dry-run, and at the baseline coefficient a blocked mutant burns
minutes of it, so a thirteen-mutant shard can fill a ninety-minute job.

### Mutation testing trend

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
  [ADR-038](../adr/ADR-038-victoriametrics-ci-trend-store.md).
- **Workflow artifacts** — every run uploads `mutation-run-status`; only a complete
  artifact set uploads `mutation-canonical-row`. Validation and the no-partial-row
  contract are implemented by
  [`mutation-status-build.sh`](../../scripts/mutation-status-build.sh) and the strict
  language merge scripts beside it.

Numeric mutation-score history lives in VictoriaMetrics + Grafana, the right
home for time-series telemetry.

An incomplete run fails as incomplete after publishing its completion status. It
does not emit a canonical language score from whichever shards happened to finish.

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

### Property-based tests

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

### Fuzzing

The wire decoder is the agent's primary untrusted-input surface, so
[`Frame::decode`](../../agent/crates/mesh-protocol/src/codec.rs) has a coverage-guided
cargo-fuzz / libFuzzer target ([`agent/fuzz/fuzz_targets/decode.rs`](../../agent/fuzz/fuzz_targets/decode.rs))
that asserts arbitrary bytes never panic. libFuzzer needs a nightly toolchain, so
the bounded session runs as observability — `make fuzz-rust` locally, and the
nightly [fuzz.yml workflow](../../.github/workflows/fuzz.yml) in CI — never as a
merge gate.

The always-run guard on stable is
[`decode_corpus_test.rs`](../../agent/crates/mesh-protocol/tests/decode_corpus_test.rs):
it replays every seed in [`agent/fuzz/corpus/decode/`](../../agent/fuzz/corpus/decode)
(crafted edge cases per decode branch, a real encoded frame, plus any minimized
crash) through the decoder under plain `cargo test`. A crash found by the nightly
fuzzer is minimized and committed back into that corpus, so the stable replay
re-runs it forever.

The server side of the same surface uses Go's native fuzzing in
[`codec_fuzz_test.go`](../../server/internal/protocol/codec_fuzz_test.go):
`FuzzReadFrame` holds the envelope parser to "no panic, and no allocation past
`MaxFrameSize`", and `FuzzDecodeControl` holds the msgpack decoder to "decode or
error, never panic". Both seed from the committed goldens — `FuzzDecodeControl`
peels the envelope first so the fuzzer starts on msgpack-shaped input — plus
hand-written edge cases for empty, truncated and unknown-type frames. Under
plain `go test` the seed corpus runs as a normal test; each doc comment carries
the `-fuzz` invocation for an extended local session.

## Frontend Performance

### Bundle Size Monitoring

`size-limit` with `@size-limit/file` enforces gzip size budgets on the production build output. Configuration: `web/.size-limit.json`.

```bash
# Check bundle size locally
cd web && npm run build && npm run size
```

### Lighthouse CI

After E2E tests, Lighthouse CI audits `/login` with 3 runs (desktop, no throttling). Accessibility and best-practices failures are hard errors; performance is warn-only due to CI runner variance. Configuration: `web/.lighthouserc.json`.

```bash
# Run locally (requires server at localhost:8080)
npm install -g @lhci/cli
cd web && lhci autorun
```

## Performance Benchmarks

The standalone [benchmark workflow](../../.github/workflows/benchmark.yml) tracks hot-path
performance trends in VictoriaMetrics. Allocation metrics (`allocs/op`, `B/op`) are
deterministic and gate against the committed
[baseline](../../benchmarks/baseline.json); wall-clock `ns/op` gates against a
VictoriaMetrics window baseline plus an absolute ceiling, sized from the live
series' measured variance because shared GitHub runners are noisy. See
[CI Pipeline](./CI-Pipeline.md) for the gate semantics.

| Language | What's Benchmarked | Tool |
|----------|--------------------|------|
| Go | Protocol codec, cert signing, DB operations, handshake | `testing.B` + `-benchmem` |
| Rust | Frame/handshake encode/decode; Edge Sentinel detection, sampler, RSS probe | Criterion 0.8 |

### Running benchmarks locally

```bash
# Go
cd server && go test -bench=. -benchmem -run='^$' ./internal/...

# Rust
cd agent && cargo bench -p mesh-protocol
cd agent && cargo bench -p mesh-agent-core --bench edge_sentinel_bench
```

### Regression model

The committed [`benchmarks/baseline.json`](../../benchmarks/baseline.json) is the reviewed
baseline. Allocation regressions above the baseline tolerance fail the workflow; `ns/op`
outliers are emitted as advisory lines and graphed on the Grafana **Benchmark Trends**
dashboard.

## End-to-End Tests (Playwright)

E2E tests run Playwright against a real server instance via [`docker-compose.test.yml`](../../deploy/docker-compose.test.yml): a Postgres container, a server container built from source, and the two machines described above. The database and the server back their state with tmpfs, so teardown is instant.

### Test suites

One spec per capability, in [`web/e2e/`](../../web/e2e/). Each names in its own
header what a technician does in it, so the directory listing is the index.

### Fixtures

Custom Playwright fixtures in `web/e2e/fixtures.ts` provide:
- `testUser` — registers a fresh user via API before each test
- `authedPage` — a page with auth token pre-injected into localStorage

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
- **Staging**: `web/playwright.staging.config.ts` — derives from the CI config and
  overrides only the target and retry policy, so the two cannot drift in how the
  suite executes; `scripts/tests/playwright-config-parity.test.sh` enforces it.
  What it does not inherit is the stack: the deploy job stands staging's
  machines up, which is why they are checked against the same pinned names

### Running locally

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
```

### Running Docker locally (credential-helper guardrail)

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

## Security & Middleware Tests

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

## Cross-Component Integration Tests

These tests exercise multi-component interaction paths that unit tests cannot cover:

### Agent SessionHandler (Rust)

[`agent/crates/mesh-agent-core/src/session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs) covers frame dispatch, permission enforcement, and error paths. A representative slice:

| Test | What It Verifies |
|------|-----------------|
| `test_handle_frame_ping_responds_pong` | Ping frame → Pong response |
| `test_handle_frame_terminal_no_session` | Terminal frame with no active session — no panic |
| `test_handle_frame_unexpected_type_ignored` | Desktop frame from browser silently ignored |
| `test_handle_control_mouse_move_permitted` | `permissions.input = true` → `InputInjector` called |
| `test_handle_control_mouse_move_denied` | `permissions.input = false` → injector NOT called |
| `test_handle_control_file_list_success` | `FileListRequest` → `FileListResponse` on channel |
| `test_handle_control_file_list_error` | `FileListRequest` for nonexistent path → `FileListError` |
| `test_handle_control_chat_echoes_back` | `ChatMessage` → echoed with `sender: "agent"` |
| `test_handle_control_chat_preserves_text` | Unicode and empty string preserved in echo |
| `test_send_frame_closed_channel` | Closed channel → `SessionError::WebSocket` |

Uses `RecordingInjector` mock that records all `inject_*` calls for assertion.

### Relay Protocol Frame Roundtrip (Go)

`server/tests/integration/relay_data_test.go` — `TestRelayProtocolFrameRoundTrip` with 4 sub-tests:

| Sub-test | Direction | Frame |
|----------|-----------|-------|
| `control_mouse_move` | browser → agent | msgpack `MouseMove{X:100,Y:200}` |
| `control_file_list_request` | browser → agent | msgpack `FileListRequest{Path:"/home"}` |
| `terminal_frame` | agent → browser | msgpack `TerminalFrame{Data:"ls -la\n"}` |
| `bidirectional_control` | both ways | Simultaneous `MouseMove` + `FileListResponse` |

Sends properly encoded `[type][4-byte BE length][payload]` frames through the full QUIC+WebSocket relay path and verifies msgpack decoding on the receiving side.

### WebRTC Signaling via Relay (Go)

[`server/tests/integration/signaling_relay_test.go`](../../server/tests/integration/signaling_relay_test.go):

| Test | What It Verifies |
|------|-----------------|
| `TestSignalingFlowThroughRelay` | Full signaling lifecycle: SDP offer → answer → ICE candidates → dual SwitchAck → tracker reaches `PhaseConnected` |
| `TestSignalingTimeout` | Offer sent, no answer → tracker records `PhaseFailed` |

Uses fake SDP strings — the relay is message-agnostic and just forwards binary frames.

### OTA Update Pipeline (Go + Rust)

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

### The triage path, agent to resolution (Go)

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

## Load Tests

A run is valid, failed, or **invalid**. Valid and failed are both measurements —
one of a system that held, one of a system that did not — and both belong in the
trend. Invalid is the third: the run did not measure the system, because a
scenario produced no rows, the generator ran out of room, or a safety ceiling
stopped it. An invalid run never enters the trend, because a partial night
absorbed as data lowers the window median and the next genuinely slow night then
compares favourably against it and passes.
[ADR-082](../adr/ADR-082-load-runs-measure-the-system-or-say-they-did-not.md) is
the decision behind that and everything below it.

### What a run is configured by, and what it produces

A run reads one profile and writes one evidence bundle.

**Profiles** live in [`load/profiles/`](../../load/profiles) and are versioned.
Each declares its family, the environment class it runs in, the fixture it needs,
an ordered phase list, the safety limits that stop it, and the gates its results
are read against. The environment vocabulary has no production member, which is
what makes "production is never a target" a property of the schema rather than of
a reviewer's attention.

The environment also decides which safety limits mean anything. The processor
ceiling is a promise made to whatever else sits on the node, so a staging profile
declares one and a disposable runner stack — created by the job and thrown away
with it — may not: driving the processor is the scaling sweep's whole experiment,
and a ceiling nothing consults reads as protection that is not there. The memory
and disk ceilings hold everywhere, because past them the node has nowhere to put
what the run produces. The schema enforces both halves.

**Bundles** are versioned JSON, one per run, uploaded as a workflow artifact. A
bundle carries what produced the numbers, on what hardware, against how much
data, what load was offered, what load arrived, what the run observed, and what
state it left the system in. The metrics store retains 30 days, so the bundle is
authoritative and the dashboard is a view of it; a bundle missing a mandatory
section fails the run rather than entering the trend as a thinner version of a
real one.

Both schemas, their validation and the verdict rules live in
[`server/tests/loadtest/`](../../server/tests/loadtest) and are exercised by that
package's tests.

### The six families, and where each runs

| Family | Venue | Why there |
|---|---|---|
| Normal / peak | Staging at night | the server is capped, and the node has real headroom |
| Spike | Staging at night | same envelope, one short burst |
| Soak | Staging overnight | the node is billed by existing, not by working |
| Breakpoint | Staging, under guardrails | saturating the node throttles production's probes, so its safety ceilings are the lowest of any profile |
| Volume | GitHub-hosted runner | staging's database shares the node root with production; a runner brings its own disk |
| Scaling | GitHub-hosted runner | the sweep needs four or five processor points and the cluster offers one |

A runner is x86_64 and production is ARM64, so the last two families produce
comparisons — between fixture sizes, or between processor counts — and never an
absolute capacity claim about production hardware. Their stack is
[`deploy/docker-compose.perf.yml`](../../deploy/docker-compose.perf.yml), driven
by [`perf-stack.yml`](../../.github/workflows/perf-stack.yml), which also weighs
the fixture it built with
[`scripts/perf-weigh-fixture.sh`](../../scripts/perf-weigh-fixture.sh).

### k6 HTTP/WS Scenarios

Three k6 scenarios in [`load/k6/scenarios/`](../../load/k6/scenarios), each declaring
its own VU ramp and thresholds in its `options` block:

| Scenario | Exercises |
|----------|-----------|
| [`api-baseline.js`](../../load/k6/scenarios/api-baseline.js) | Health, current user, sites, and the device list under a steady ramp |
| [`relay-throughput.js`](../../load/k6/scenarios/relay-throughput.js) | A real remote session: the operator's side of the relay, timing its own frame coming back from the machine |
| [`concurrent-agents.js`](../../load/k6/scenarios/concurrent-agents.js) | Agent-shaped device and session reads spread across the fleet's sites |

`setup()` registers a throwaway member of the staging organization through
[`load/k6/lib/session.js`](../../load/k6/lib/session.js) and reads the sites that
member can see. Where a scenario times a journey against one machine, it reads
the fleet from a site chosen for holding machines: an estate spreads its fleet
over its sites, so a site picked for sorting first is empty as often as not, and
a journey with nothing to open publishes a zero indistinguishable from a fast
night. The scenarios drive read paths only: organization is the visibility
boundary, so a member reads the whole fleet, while creating a site is administrator
work the server refuses. A scenario that stood up its own fixtures would measure the
403 path instead. `setup()` throws on an unexpected status, so a broken precondition
names itself rather than turning every request in the run red.

Every name a run creates carries the run's marker and its own seed, so two nights
never ask the server for the same customer.
[`scripts/loadtest-cleanup.sh`](../../scripts/loadtest-cleanup.sh) removes what
matches — accounts, customers, sites and machines — and counts each kind, and
that count travels in the bundle: a run that says it left nothing has to have
looked, and a kind that is removed but never counted is a kind whose residue
nobody can see. The statements are held to the live schema by a test in
[`server/tests/loadtest/`](../../server/tests/loadtest) that runs the script
against a database the migrations built, so a column that moves fails the day it
moves rather than the next night.

The relay scenario needs a machine on the other end of every session it opens, so
the QUIC harness holds its fleet connected for the whole k6 window rather than
running after it. What the metric records is the operator's own frame going out
through the server, through the machine, and back. The relay is a byte pipe —
the agent protocol is MessagePack, so the server forwards every frame as binary
— and k6 delivers a binary frame to a different handler than a text one, so the
operator's side listens on both. Listening on one leaves the echo arriving at a
handler that does not exist: the frame lands and is counted, the round trip is
never recorded, and each iteration spends its whole echo timeout waiting for an
answer it already had.

Each scenario declares the workload it performs, and that name travels with every
sample into the trend store. A window baseline is only a baseline for the work
that produced it, so a scenario rewritten to measure something else takes a new
name and is compared against itself rather than against what it replaced. See
[ADR-092](../adr/ADR-092-a-trend-series-carries-the-workload-that-produced-it.md).

The scenarios spell their URLs by hand, so
[`scripts/tests/api-endpoint-drift.test.sh`](../../scripts/tests/api-endpoint-drift.test.sh)
checks every path and query parameter they send — and every one
[`deploy/scripts/smoke-test.sh`](../../deploy/scripts/smoke-test.sh) probes — against
[`api/openapi.yaml`](../../api/openapi.yaml), which is what makes a route rename fail
in the gauntlet rather than in the nightly.

[`scripts/loadtest-k6-run.sh`](../../scripts/loadtest-k6-run.sh) runs each scenario and
keeps its summary export only when the run produced a measurement. A failed
threshold counts as one; a script exception does not, and its export is discarded so
the handful of requests `setup()` managed never enters the trend the regression
check compares against. A breached threshold is recorded beside the export rather
than failing the scenario, because whether a mark is blocking is the profile's
decision — a mark set tighter than the measurement's own spread would otherwise
fail every night from the day it was tightened.

Against staging, k6 itself runs in a short-lived cluster pod through
[`scripts/loadtest-k6-incluster.sh`](../../scripts/loadtest-k6-incluster.sh), which
executes the same k6 argument list beside the server and copies the summary export
back to the runner for the decision above. Generating load one hop from the server
is what keeps the trend a measurement of the server rather than of the path to it.
The generator pod holds its own processor and memory allocation, separate from the
server's: sharing them is why the API mark had to be set wider than any regression
worth finding.

### Go QUIC Load Harness

[`server/tests/loadtest/`](../../server/tests/loadtest) connects N machines, each
performing the full mTLS QUIC handshake and registration, then holds them
connected and behaving — heartbeats and telemetry on a jittered cadence,
reconnects with a backlog to drain, duplicate connections, and the machine side
of any relay session the server hands it. It reports p50/p95/p99 for connect,
handshake and register, and writes an evidence bundle.

It also reports the window the fleet arrived in — from the run's start to the
moment the last machine finished registering — and that is what the run's arrival
rate divides by. The run's own wall clock is nearly all hold: `-hold` keeps every
machine connected so the k6 relay scenario has something to open sessions
against, so a rate taken from it describes the hold rather than the arrival, and
a hundred machines that all arrived report a fraction of one per second.

Registration is the one figure it does not time itself. The harness's own clock
would stop when the frame reaches a local send buffer, which reports microseconds
however slow the write behind it becomes, and the device row is written later and
elsewhere — so `-metrics-url` points it at the server's own account of how long
registration took, published where that row lands, with the connection pool
beside it. A registration queued behind a connection and one executing slowly are
the same latency until the pool says which. That figure is the one the run
publishes: a run the server did not answer publishes no registration line at all,
because an absent figure is honest where a local write under registration's name
is not.

```bash
# Default: 100 machines against a local stack that owns its own authority
cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090

# Against staging: build the fleet through the API as the seeded service
# account, enrol the way an installer does, hold the fleet connected, and answer
# session requests so a generator can measure the relay
cd server && go run ./tests/loadtest/ \
  -agents=500 -addr=10.0.0.42:9090 \
  -enroll-url=http://opengate-staging-server:8080 \
  -metrics-url=http://opengate-staging-server:8080 \
  -fixture-account="$SERVICE_ACCOUNT" -fixture-password="$SERVICE_PASSWORD" \
  -relay-sessions -hold=8m \
  -profile=../load/profiles/normal.yaml -bundle=/tmp/loadtest-bundle
```

A run given a profile walks its phases — climbing to each declared level, holding
there, and winding down at the end — rather than offering the whole fleet at once
and waiting. The machine it shares is read between phases against the profile's
own limits, and a run that has pushed it past them stops there.

The fleet itself is built through the same interface a technician uses:
customers, sites and operator accounts are ordinary requests, and the machines
arrive by enrolling with a credential the run mints and spends. `-fixture-size`
picks which of the three committed fleets to build and `-fixture-seed` decides
it, so the same seed reproduces the same fleet exactly.

The certificate authority's private key never leaves the cluster. Against
anything shared the harness keeps its own private keys and sends signing
requests, spending a token minted for the run and deleted after it; only a local
stack, whose authority is as disposable as the stack around it, is signed for
directly.

Every address the harness dials goes through one allowlist — the configured
target and the relay URL that arrives inside a session request alike — so a field
on the wire cannot send a generator somewhere the run is forbidden to go.

[`scripts/loadtest-quic-run.sh`](../../scripts/loadtest-quic-run.sh) applies the
same keep-or-discard rule the k6 half has: a fleet that half connected is a
measurement and its error rate is the finding, while a harness that could not
start describes its own failure and its output is discarded.

[`scripts/loadtest-run-completeness.sh`](../../scripts/loadtest-run-completeness.sh)
then names which scenarios produced rows and which did not, and returns the
verdict that decides whether the night enters the trend at all.

### CI integration

- **E2E** runs on every push and gates `merge-to-main` (includes Lighthouse CI audits)
- **Bundle size** runs on every push and gates `merge-to-main` (size-limit gzip check)
- **Load tests** run nightly at 05:00 UTC and on `workflow_dispatch` (not on every push)
- **The performance stack** — the volume and scaling families, on a throwaway
  runner — runs nightly at 07:00 UTC, clear of the twenty-job pool the mutation
  matrix holds from 03:00
- **Browser performance evidence** comes from Lighthouse CI artifacts/summaries
  and the bundle-size gate; PageSpeed Insights is not part of the current CD
  workflow.

## Investigating Test Failures in Production

When a test passes in CI but something misbehaves in staging/production, reach for the `/observe` Claude Code skill (`.claude/skills/observe/SKILL.md`) or the same kubectl/Loki paths documented in [Monitoring](./Monitoring.md#ad-hoc-investigation). The current stack is Kubernetes-native, so starting signals come from pod health, Service port-forwards, and Loki labels rather than Docker container state:

| Playbook | Starting signals |
|----------|-----------------|
| Agent offline | Agent service status on the device, QUIC reachability, Loki `enroll`/`agent` entries |
| Requests slow | p95/p99 latency, slowest routes, DB latency by operation, node CPU/memory |
| Deployment health | Deployment rollout status, `/api/v1/health`, `agents_connected`, ingress 5xx |
| Post-deploy verification | new errors since deploy timestamp, server image tag from the Kubernetes Deployment |

See [[Monitoring]] for the underlying PromQL/LogQL query patterns.
