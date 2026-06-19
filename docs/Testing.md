# Testing

The project follows a strict test-first approach. All logic is covered before shipping, the Go test runner always uses `-race`, and every `db.Store` test wipes its state before running — no shared state between test cases.

### Database Tests

`db.Store` contract tests run against a real PostgreSQL 17 service container. The shared tests in [`server/internal/db/store_test.go`](../server/internal/db/store_test.go) use a factory pattern and `storeFactories` table. Integration and handler tests obtain stores through [`server/internal/testutil/testutil.go`](../server/internal/testutil/testutil.go)'s `NewTestStore(t)`, which creates a fresh PostgreSQL schema (`ogt_<uuid>`) per test, runs migrations on it, and drops it on cleanup — so tests may safely call `t.Parallel()`. Each test pool caps at 3 connections and a process-wide semaphore limits concurrent live stores; with the default Postgres `max_connections=100` the working set can saturate when many parallel tests overlap, so the project's Makefile (`make postgres-test-up`) and CI launch Postgres with `-c max_connections=400`. Tests are skipped when `POSTGRES_TEST_URL` is unset (so `go test ./internal/db/...` without a local Postgres exits cleanly).

To run the DB tests locally:

```bash
# Start a test Postgres with the required max_connections
make postgres-test-up

# Run with the Postgres URL set
POSTGRES_TEST_URL="postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable" \
  go test -race -timeout 5m ./internal/db/...
```

## Test Layers

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Load Tests                                 │
│  k6 HTTP/WS scenarios + Go QUIC harness (staging, on-demand)        │
├─────────────────────────────────────────────────────────────────────┤
│                         E2E (Playwright)                            │
│  Auth flows, device list, settings panel, navigation — via docker      │
├─────────────────────────────────────────────────────────────────────┤
│                        Golden (cross-language)                      │
│  Rust generates binary fixtures  →  Go verifies bit-identical       │
├─────────────────────────────────────────────────────────────────────┤
│                        Integration                                  │
│  HTTP round-trips + real QUIC + live Postgres (in-process server)   │
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
| **Integration** | `httptest` + real QUIC + live Postgres | `server/tests/integration/` |
| **Golden** | Rust generates → Go verifies | `testdata/golden/` |
| **Web Unit** | Vitest + React Testing Library | `web/src/**/*.test.{ts,tsx}` |
| **Web Integration** | Vitest + React Testing Library | `web/tests/integration/**/*.test.tsx` |
| **E2E** | Playwright (Chromium) | `web/e2e/*.spec.ts` |
| **Load (HTTP)** | k6 | `load/k6/scenarios/` |
| **Load (QUIC)** | Go harness | `server/tests/loadtest/` |

## Running Tests Locally

```bash
make test               # All tests — Rust + Go + Web
make test-go            # Go server (unit + integration) with race detector
make test-integration   # Integration suite only
make test-rust          # Rust workspace
make test-web           # React / TypeScript
make test-coverage      # Go coverage report printed to stdout
make golden             # Regenerate golden fixtures and verify cross-language compat
make e2e                # Playwright E2E via docker-compose.test.yml
make load-test          # k6 HTTP/WS load tests against localhost:8080
make load-test-quic     # Go QUIC load harness (100 concurrent agents)
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

All three languages enforce **80% minimum line coverage** both in CI and locally (via `/precommit`):

```bash
# Go — coverage with exclusions (testutil, metrics, openapi_gen)
cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
grep -v -E '/(testutil|metrics)/|api/openapi_gen\.go' coverage.out > coverage-prod.out
go tool cover -func=coverage-prod.out | grep total

# Web — Vitest v8 coverage, check summary JSON
cd web && npx vitest run --coverage
node -e "const s=require('./coverage/coverage-summary.json');const l=s.total.lines.pct;console.log('Web line coverage: '+l+'%');process.exit(l<80?1:0)"

# Rust — cargo-llvm-cov with exclusions
cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
  --ignore-filename-regex '(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)'
```

### Mutation testing

Coverage % asserts which lines executed; mutation score asserts which lines
were *meaningfully* tested. Run `make mutate` to drive cargo-mutants (Rust),
gremlins (Go), and stryker (Web).

**Carve-outs** (genuinely unmutateable code, analogous to platform shims):
- Rust: [agent/.cargo/mutants.toml](../agent/.cargo/mutants.toml) — platform
  shims, agent binary entry point, SELinux restorecon match guards.
- Go:   [server/.gremlins.yaml](../server/.gremlins.yaml) — `openapi_gen.go`,
  `cmd/meshserver/main.go`, `tests/loadtest/main.go`, `internal/testutil/`.
- Web:  [web/stryker.config.json](../web/stryker.config.json) — `main.tsx`,
  `router.tsx`, `use-terminal.ts`, `use-remote-desktop.ts`,
  `input-handler.ts`, `state/connection-store.ts` (WebRTC paths jsdom can't
  simulate).

Rust runs need `OPENGATE_GOLDEN_DIR=<repo>/testdata/golden` so golden file
tests resolve fixtures inside cargo-mutants' temp tree. The `mutate-rust`
make target sets this automatically.

### Mutation testing trend

Mutation tests do **not** gate merges or deploys. They run **nightly** via the
[mutation.yml workflow](../.github/workflows/mutation.yml) and
emit a row per run to:

- **VictoriaMetrics** — mapped by
  [`scripts/mutation-vm-push.sh`](../scripts/mutation-vm-push.sh) and sent through
  the shared [`vm-push.sh`](../scripts/lib/vm-push.sh) transport. Visualised by
  the provisioned
  [`mutation-trend.json`](../deploy/grafana/provisioning/dashboards/mutation-trend.json)
  dashboard. Canonical trend store per
  [ADR-038](./adr/ADR-038-victoriametrics-ci-trend-store.md).
- **Workflow artifact** — each run uploads `mutation-canonical-row` (the
  per-run JSON object) with 90-day retention for one-off audits.

The previous in-repo `docs/mutation-history.jsonl` was removed: the bot push
that maintained it was rejected by branch protection on `dev` (required
status checks block direct bot commits), and VictoriaMetrics + Grafana is the
right home for numeric time-series telemetry.

**Regression alert rules** — fired when any language regresses on either
condition:
- absolute score drops below **85%**, or
- score drops more than **2 percentage points** from the previous successful
  run.

The `no_coverage` field is reported as `—` for Rust: cargo-mutants does not
distinguish "missed" from "not covered" — every untested mutant lands in
`missed` / `Survived`. The field is preserved in the canonical row for shape
consistency across languages but encoded as `null`.

On regression the workflow goes red ❌ in the GitHub Actions history and
sends a Telegram alert via the existing `DEPLOY_TELEGRAM_BOT_TOKEN` /
`DEPLOY_TELEGRAM_CHAT_ID` secrets. Nothing else blocks; `merge-to-main`
remains independent.

The full design rationale is in
[`.claude/plans/pr9-mutation-testing-as-observability.md`](../.claude/plans/archive/pr9-mutation-testing-as-observability.md).

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

The standalone [benchmark workflow](../.github/workflows/benchmark.yml) tracks hot-path
performance trends in VictoriaMetrics. Allocation metrics (`allocs/op`, `B/op`) are the
deterministic regression gate; wall-clock `ns/op` is advisory because shared GitHub
runners are noisy.

| Language | What's Benchmarked | Tool |
|----------|--------------------|------|
| Go | Protocol codec, cert signing, DB operations, handshake | `testing.B` + `-benchmem` |
| Rust | Frame encode/decode, handshake encode/decode | Criterion 0.5 |

### Running benchmarks locally

```bash
# Go
cd server && go test -bench=. -benchmem -run='^$' ./internal/...

# Rust
cd agent && cargo bench -p mesh-protocol
```

### Regression model

The committed [`benchmarks/baseline.json`](../benchmarks/baseline.json) is the reviewed
baseline. Allocation regressions above the baseline tolerance fail the workflow; `ns/op`
outliers are emitted as advisory lines and graphed on the Grafana **Benchmark Trends**
dashboard.

## End-to-End Tests (Playwright)

E2E tests run Playwright against a real server instance via `deploy/docker-compose.test.yml`. The test environment uses a server container (built from source) plus a Postgres 17 container — both back their state with tmpfs, so teardown is instant.

### Test suites

| Suite | Tests | Description |
|-------|-------|-------------|
| `auth.spec.ts` | 5 | Register, login (valid/invalid), logout, expired token |
| `device-list.spec.ts` | 3 | Empty state, group creation, device listing |
| `admin.spec.ts` | 4 | Non-admin blocked from /settings, user list, audit log, sidebar sections |
| `navigation.spec.ts` | 4 | Unauthenticated redirects, SPA routing |
| `security-permissions.spec.ts` | 6 | Security groups, permissions, access control |

### Fixtures

Custom Playwright fixtures in `web/e2e/fixtures.ts` provide:
- `testUser` — registers a fresh user via API before each test
- `authedPage` — a page with auth token pre-injected into localStorage

### Configuration

- **CI**: `web/playwright.config.ts` — targets `http://localhost:8080` (docker-compose)
- **Staging**: `web/playwright.staging.config.ts` — targets `http://127.0.0.1:18080` (SSH tunnel)

### Running locally

```bash
# Bring up test server, run Playwright, tear down
make e2e

# Or manually:
cd deploy && docker compose -f docker-compose.test.yml up -d --build --wait
cd web && npx playwright test
cd deploy && docker compose -f docker-compose.test.yml down -v
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

[`scripts/docker-credstore-guard.sh`](../scripts/docker-credstore-guard.sh)
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
| `server/internal/api/handlers_auth_test.go` | Email validation (invalid formats rejected, valid formats accepted) |
| `web/src/components/ErrorBoundary.test.tsx` | Error boundary renders fallback UI on child component crash |
| `server/tests/integration/middleware_ws_test.go` | Full middleware stack preserves `http.Hijacker` for WS upgrades, relay route bypasses 30s `RequestTimeout` |

The existing 22 Playwright E2E tests continue to pass with the auth rate limiter active, confirming no regressions from the new middleware.

## Cross-Component Integration Tests

These tests exercise multi-component interaction paths that unit tests cannot cover:

### Agent SessionHandler (Rust)

`agent/crates/mesh-agent-core/src/session/handler.rs` — 8 unit tests covering frame dispatch, permission enforcement, and error paths:

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

`server/tests/integration/signaling_relay_test.go` — 2 tests:

| Test | What It Verifies |
|------|-----------------|
| `TestSignalingFlowThroughRelay` | Full signaling lifecycle: SDP offer → answer → ICE candidates → dual SwitchAck → tracker reaches `PhaseConnected` |
| `TestSignalingTimeout` | Offer sent, no answer → tracker records `PhaseFailed` |

Uses fake SDP strings — the relay is message-agnostic and just forwards binary frames.

### OTA Update Pipeline (Go + Rust)

**Go** (`server/tests/integration/update_test.go`) — 3 tests:

| Test | What It Verifies |
|------|-----------------|
| `TestUpdatePublishAndPush` | Admin publishes manifest → pushes update → agent receives `AgentUpdate` on QUIC → sends `AgentUpdateAck` → DB records update |
| `TestUpdatePushSkipsCurrentVersion` | Agent already on target version → `pushed_count=0` |
| `TestUpdatePushNoMatchingOS` | Manifest for windows/amd64, agent is linux/amd64 → not pushed |

**Rust** (`agent/crates/mesh-agent-core/src/update.rs`) — 1 integration test:

| Test | What It Verifies |
|------|-----------------|
| `test_apply_update_full_pipeline` | Mock HTTP server serves fake binary → `apply_update()` downloads, verifies SHA-256, validates Ed25519 signature, backs up old binary to `.prev`, replaces current binary, writes `.update-pending` sentinel |

## Load Tests

### k6 HTTP/WS Scenarios

Three k6 scenarios in `load/k6/scenarios/`:

| Scenario | VUs | Duration | Thresholds |
|----------|-----|----------|------------|
| `api-baseline.js` | 50 | 2 min | p95 < 200ms, errors < 0.1% |
| `relay-throughput.js` | 20 | 1 min | p95 relay latency < 50ms |
| `concurrent-agents.js` | 100 | 2 min | p99 < 500ms, errors < 0.1% |

Each scenario registers a test user in `setup()`, then exercises API endpoints (health, groups, devices, sessions) under load.

### Go QUIC Load Harness

`server/tests/loadtest/main.go` spawns N concurrent goroutines, each performing the full mTLS QUIC handshake and agent registration. Reports p50/p95/p99 latency for connect, handshake, and register phases.

```bash
# Default: 100 agents against localhost
cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090

# Custom agent count and address
cd server && go run ./tests/loadtest/ -agents=500 -addr=staging.example.com:9090
```

### CI integration

- **E2E** runs on every push and gates `merge-to-main` (includes Lighthouse CI audits)
- **Bundle size** runs on every push and gates `merge-to-main` (size-limit gzip check)
- **Load tests** run on `workflow_dispatch` and weekly schedule only (not on every push)
- **Browser performance history** comes from Lighthouse CI and the non-blocking
  `perf-publish` path; PageSpeed Insights is not part of the current CD workflow.

## Investigating Test Failures in Production

When a test passes in CI but something misbehaves in staging/production, reach for the `/observe` Claude Code skill (`.claude/skills/observe/SKILL.md`) or the same kubectl/Loki paths documented in [Monitoring](./Monitoring.md#ad-hoc-investigation). The current stack is Kubernetes-native, so starting signals come from pod health, Service port-forwards, and Loki labels rather than Docker container state:

| Playbook | Starting signals |
|----------|-----------------|
| Agent offline | Agent service status on the device, QUIC reachability, Loki `enroll`/`agent` entries |
| Requests slow | p95/p99 latency, slowest routes, DB latency by operation, node CPU/memory |
| Deployment health | Deployment rollout status, `/api/v1/health`, `agents_connected`, ingress 5xx |
| Post-deploy verification | new errors since deploy timestamp, server image tag from the Kubernetes Deployment |

See [[Monitoring]] for the underlying PromQL/LogQL query patterns.
