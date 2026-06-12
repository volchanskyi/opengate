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

- **Loki** — pushed via the existing deploy SSH tunnel into the monitoring
  docker network. Visualised in Grafana under the "Mutation Testing Trend"
  dashboard (uid `opengate-mutation-trend`). Canonical trend store.
- **Workflow artifact** — each run uploads `mutation-canonical-row` (the
  per-run JSON object) with 90-day retention for one-off audits.

The previous in-repo `docs/mutation-history.jsonl` was removed: the bot push
that maintained it was rejected by branch protection on `dev` (required
status checks block direct bot commits), and Loki + Grafana is the right
home for time-series telemetry.

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

CI tracks performance of hot paths across commits to catch regressions. Results are stored in the `gh-pages` branch via [github-action-benchmark](https://github.com/benchmark-action/github-action-benchmark).

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

### Regression threshold

**120%** — the bench workflow fails if any benchmark is more than 20% slower than the stored baseline. Historical charts are viewable on GitHub Pages.

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
public images pull anonymously. It is wired into `make e2e`,
[`scripts/e2e-multiserver.sh`](../scripts/e2e-multiserver.sh), and the precommit
gauntlet. It is a **no-op** when the helper works or none is configured, so CI
(where docker login writes `auths` directly, with no broken `credsStore`) is
unaffected. To run any ad-hoc docker command through the same guard:

```bash
DOCKER_CONFIG="$(scripts/docker-credstore-guard.sh)" docker compose ...
```

If you prefer a permanent local fix instead, remove the dead `credsStore` line
from `~/.docker/config.json` (Docker then pulls public images anonymously and
still honours any `auths` entries).

## Multiserver E2E

The multiserver harness proves the cross-server relay proxy ([ADR-023 Amendment 2](./adr/ADR-023-relay-extraction-redis-session-registry.md#amendments)) and the Redis-loss degraded-mode posture ([ADR-023](./adr/ADR-023-relay-extraction-redis-session-registry.md)) end-to-end against **two real server replicas** sharing one Redis and one Postgres — behaviours that unit tests (miniredis, fake dialer, `httptest`) can only approximate. The topology is `deploy/docker-compose.multiserver.yml`; the host-side wire driver is `server/tests/e2e-multiserver/`.

It is a `main` package (not `*_test.go`), so it stays out of `go test ./...` and is driven only by the Makefile target — mirroring how Playwright E2E lives outside the unit suite. The driver refuses to run unless `OPENGATE_MULTISERVER_E2E=1` (set by the orchestration script).

| Scenario | Asserts |
|----------|---------|
| `cross-server-frame-flow` | Agent on replica A + browser on replica B relay bytes both directions through the affinity owner's internal listener |
| `owner-death-ttl-reclaim` | SIGKILL the affinity owner mid-session; a same-token pair reclaims on the surviving replica once the stale affinity claim's TTL lapses (reconnect-with-fresh-token contract — no live migration) |
| `redis-death-degraded-refuse` | On Redis loss the `opengate_registry_up` gauge flips to 0 (the alert signal), the in-flight session keeps relaying (drains), new sessions are refused with WebSocket close `1013`, and the system recovers when Redis returns |

```bash
# Build + up --wait + run all three scenarios + guaranteed teardown
make e2e-multiserver
```

`scripts/e2e-multiserver.sh` owns the lifecycle (it shortens the relay's 30s degraded/affinity timers via `OPENGATE_DEGRADED_THRESHOLD` / `OPENGATE_AFFINITY_TTL` env so the fault scenarios run in seconds, and dumps container logs on failure before teardown). CI runs it nightly via `.github/workflows/e2e-multiserver.yml` (`workflow_dispatch` + cron) — **not** on the `merge-to-main` path.

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

### Multiserver relay latency baseline

`make load-test-multiserver` reuses the multiserver topology and harness (`E2E_LOAD_SAMPLES` switches the driver into load mode) to measure steady-state **one-way relay latency** for two routing modes, quantifying the extra intra-cluster hop the cross-server proxy adds — the answer to [ADR-023](./Architecture-Decision-Records.md)'s "revisit the affinity TTL after the first load test".

First baseline (2026-06-05, single-host loopback, 200 samples each — network RTT is ~0, so these isolate the relay/proxy software cost, not real inter-node latency):

| Mode | p50 | p95 | p99 |
|------|-----|-----|-----|
| direct (same replica, zero-hop) | 253µs | 390µs | 580µs |
| proxied (cross replica) | 324µs | 627µs | 1.21ms |
| **proxied − direct delta** | **+70µs** | — | **+633µs** |

**TTL verdict:** the proxy software overhead is sub-millisecond and the proxied p99 (~1.2ms) sits far under the `relay-throughput.js` 50ms threshold, so the 30s `DefaultAffinityTTL` needs no change — it is orders of magnitude larger than any per-frame proxy cost, and the `owner-death-ttl-reclaim` scenario confirms reclaim works correctly. On a real multi-node cluster the proxied delta grows by one intra-VCN network RTT (same-region, typically sub-millisecond), which remains well within budget.

### CI integration

- **E2E** runs on every push and gates `merge-to-main` (includes Lighthouse CI audits)
- **Bundle size** runs on every push and gates `merge-to-main` (size-limit gzip check)
- **Load tests** run on `workflow_dispatch` and weekly schedule only (not on every push)
- **Multiserver E2E** runs on `workflow_dispatch` and a nightly schedule only (not on `merge-to-main`)
- **PageSpeed Insights** runs during staging CD deploy (requires `PSI_API_KEY`)

## Investigating Test Failures in Production

When a test passes in CI but something misbehaves in staging/production, reach for the `/observe` Claude Code skill (`.claude/skills/observe/SKILL.md`) instead of SSHing by hand. It wraps PromQL/LogQL/`docker exec` queries around the self-hosted monitoring stack and ships with ready-made playbooks for common failure shapes:

| Playbook | Starting signals |
|----------|-----------------|
| Agent offline | `systemctl status mesh-agent`, QUIC :9090 reachability, Loki `enroll`/`agent` entries |
| Requests slow | p95/p99 latency, slowest routes, DB latency by operation, host CPU/memory |
| Deployment health | container uptime vs. deploy window, `/api/v1/health`, `agents_connected`, Caddy 5xx |
| Post-deploy verification | new errors since deploy timestamp, image tag via `docker inspect` |

See [[Monitoring]] for the underlying PromQL/LogQL query patterns.
