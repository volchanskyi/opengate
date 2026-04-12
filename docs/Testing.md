# Testing

The project follows a strict test-first approach. All logic is covered before shipping, the Go test runner always uses `-race`, and every test gets its own ephemeral database — no shared state between test cases.

### Dual-Backend Database Tests

The `db.Store` contract is verified against both SQLite and PostgreSQL using a factory pattern in `server/internal/db/store_test.go`. Each shared test runs once per backend via `storeFactories`:

- **SQLite** — ephemeral temp-dir database per test (always runs)
- **PostgreSQL** — shared store with per-test `TRUNCATE ... CASCADE` isolation (runs when `POSTGRES_TEST_URL` is set)

SQLite-specific tests (corrupt UUID/timestamp scanning, WAL mode, DB accessor) live in `sqlite_only_test.go`. To run both backends locally:

```bash
# Start a test Postgres instance
docker run -d --rm --name opengate-pg-test \
  -e POSTGRES_USER=opengate -e POSTGRES_PASSWORD=opengate -e POSTGRES_DB=opengate \
  -p 55432:5432 postgres:17-alpine

# Run with both backends
POSTGRES_TEST_URL="postgres://opengate:opengate@localhost:55432/opengate?sslmode=disable" \
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
│  HTTP round-trips + real QUIC + live SQLite (in-process server)     │
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
| **Integration** | `httptest` + real QUIC + live SQLite | `server/tests/integration/` |
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
# Go — coverage with exclusions (testutil, metrics, mps/wsman, openapi_gen)
cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
grep -v -E '/(testutil|metrics|mps/wsman)/|api/openapi_gen\.go' coverage.out > coverage-prod.out
go tool cover -func=coverage-prod.out | grep total

# Web — Vitest v8 coverage, check summary JSON
cd web && npx vitest run --coverage
node -e "const s=require('./coverage/coverage-summary.json');const l=s.total.lines.pct;console.log('Web line coverage: '+l+'%');process.exit(l<80?1:0)"

# Rust — cargo-llvm-cov with exclusions
cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
  --ignore-filename-regex '(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)'
```

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

E2E tests run Playwright against a real server instance via `deploy/docker-compose.test.yml`. The test environment uses a single server container built from source with tmpfs storage.

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
