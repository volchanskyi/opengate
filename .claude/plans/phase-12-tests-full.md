# Phase 12: Integration & E2E Tests

## Context

OpenGate has grown through 11 phases — Rust agent, Go server, React web client, WebRTC, Intel AMT MPS, admin dashboard, CD pipeline — but comprehensive end-to-end testing is still missing. The codebase has 191 Go tests (13 integration), 66 Rust tests, and ~35 web test files, but no Playwright browser tests, no load testing, and a known flaky test. Phase 12 closes this gap before the project scales further.

---

## Workstream 1: Fix Flaky Test (first)

**File:** `server/tests/integration/session_test.go:345`

**Root cause:** `TestSessionLifecycle_ConcurrentSessions` connects 3 agents sequentially via `connectAgent()`, which pre-seeds each device (line 120 of `agentapi_test.go`) and then immediately sends `AgentRegister` without waiting for server processing. The server's `handleRegister` calls `UpsertDevice` asynchronously. With SQLite `MaxOpenConns(1)`, concurrent writes serialize, but the interleaving of pre-seeds and async registrations can trigger FOREIGN KEY or busy-timeout failures.

**Fix:**
1. Pre-seed all 3 devices **before** any QUIC connection — extract a `connectAgentWithExistingDevice(t, deviceID)` variant that skips the pre-seed
2. Or: give each agent its **own group** to eliminate shared-row contention
3. Validate: `go test -race -count=50 -timeout 10m ./tests/integration/ -run TestSessionLifecycle_ConcurrentSessions`

---

## Workstream 2: Expand Go Integration Tests (~30 new tests)

All in `server/tests/integration/`. Reuse existing `newAgentTestEnv`/`newSessionTestEnv` patterns and `testutil` helpers.

### New test helpers in `server/internal/testutil/testutil.go`
- `SeedAdminUser(t, ctx, store) *db.User` — admin user with real bcrypt hash
- `SeedAMTDevice(t, ctx, store) *db.AMTDevice` — AMT device record
- `GenerateJWT(t, cfg, user) string` — JWT token for a user

### New test files

| File | Tests | Covers |
|------|-------|--------|
| `reconnect_test.go` | `TestAgentReconnect_AfterDisconnect`, `_NewCert`, `_SessionSurvivesReconnect` | Agent reconnection, cert rotation, session persistence |
| `relay_data_test.go` | `TestRelay_BinaryPayloadIntegrity`, `_LargeFrameSequence`, `_BidirectionalConcurrent` | Relay data integrity (1 MB payloads, SHA-256 verification, message ordering) |
| `auth_edge_test.go` | `TestAuth_ExpiredJWT_AllEndpoints`, `_DeletedUser`, `_MalformedJWT`, `_WrongSecret`, `_DuplicateRegistration` | Auth edge cases across all protected endpoints |
| `admin_test.go` | `TestAdmin_UserPromotion`, `_AuditLog_CapturesActions`, `_AuditLog_Filtering`, `_AuditLog_Pagination` | Admin operations, audit log queries |
| `amt_test.go` | `TestAMT_ListDevices_Empty`, `_WithSeeded`, `_GetDevice_NotFound`, `_PowerAction_DeviceNotConnected` | AMT HTTP API layer (no real AMT device — DB-seeded + mock AMTOperator) |
| `signaling_test.go` | `TestSignaling_StartAndAck`, `_RecordFailure`, `_ConcurrentSessions` | WebRTC signaling tracker cross-package interaction |

### Add to existing `server/tests/integration/session_test.go`
- `TestSessionLifecycle_SessionForOfflineDevice` → 409
- `TestSessionLifecycle_SessionForNonexistentDevice` → 404
- `TestSessionLifecycle_DeleteNonexistentSession` → 404

**Total:** ~30 new integration tests (13 → ~43)

---

## Workstream 3: Docker Compose Test Environment

### New file: `deploy/docker-compose.test.yml`

```yaml
services:
  server:
    build:
      context: ..
      dockerfile: Dockerfile
    container_name: opengate-test-server
    environment:
      - JWT_SECRET=e2e-test-secret-minimum-32-bytes!
      - OPENGATE_TEST_MODE=true
    command: ["-listen", ":8080", "-data-dir", "/data"]
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/api/v1/health"]
      interval: 5s
      timeout: 3s
      retries: 10
      start_period: 10s
    tmpfs:
      - /data    # fresh DB every restart
```

Key decisions:
- **Build from source** (not GHCR image) — tests current branch
- **No Caddy** — Playwright hits server directly on :8080 (no TLS complexity)
- **No real agent** — no runnable agent binary exists yet
- **`tmpfs: /data`** — ephemeral database, each `docker compose down/up` = clean slate

### New file: `deploy/scripts/wait-for-server.sh`
- Polls `/api/v1/health` until ready or timeout (30s max)

---

## Workstream 4: Playwright E2E Tests

### Setup

| File | Purpose |
|------|---------|
| `web/playwright.config.ts` | Config: baseURL `http://localhost:8080`, Chromium only in CI |
| `web/playwright.staging.config.ts` | Staging config: baseURL `http://127.0.0.1:18080` |
| `web/e2e/fixtures.ts` | Custom Playwright fixtures (authenticated page, API helper) |
| `web/e2e/helpers/api-helper.ts` | Direct HTTP calls for test data setup (register, login, create group) |
| `web/e2e/helpers/auth-helper.ts` | Login shortcut (register + login + store JWT) |

### Test specs (~15 tests)

**Scope decision:** No runnable agent binary exists yet (only library crates). Device-present tests are skipped — Playwright tests auth flows, routing, empty states, and admin panel. Device connectivity is fully covered by Go integration tests.

| Spec | Tests |
|------|-------|
| `e2e/auth.spec.ts` | register, login valid/invalid, logout, expired token redirect |
| `e2e/device-list.spec.ts` | empty state (no groups), create group via API + verify sidebar, group selected shows empty device list |
| `e2e/admin.spec.ts` | non-admin blocked from /admin, admin user list visible, audit log view |
| `e2e/navigation.spec.ts` | SPA routing (/login, /devices, /admin), 404 fallback, auth guard redirect |

**What we DON'T test in Playwright:** WebSocket relay, WebRTC, remote desktop, device-present UI, session view — these require a real agent or are covered by Go integration tests.

**Test isolation:** Each spec registers a fresh user with unique email (timestamp + random). No cross-spec state leakage.

### Package additions to `web/package.json` devDependencies:
- `@playwright/test`

---

## Workstream 5: Load Testing (k6 + Go QUIC Harness)

### Directory: `load/k6/scenarios/`

| Scenario | VUs | Duration | Thresholds |
|----------|-----|----------|------------|
| `api-baseline.js` | 50 | 2 min | p95 < 200ms, error rate < 1% |
| `relay-throughput.js` | 20 WS sessions | 1 min | p95 < 50ms/msg, >10 MB/s aggregate |
| `concurrent-agents.js` | 100 HTTP | 2 min | p99 < 500ms, 0 errors |

**Note:** k6 tests HTTP/WebSocket only (not QUIC).

### Go QUIC Load Harness

**File:** `server/tests/loadtest/main.go`

- Spawns N goroutines, each performing the full QUIC handshake + registration
- Measures: connection time, handshake latency, registration latency
- Targets: 100 concurrent agents in < 10 seconds total setup time
- Invoked: `go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090`
- Reuses `cert.Manager` and `protocol.Codec` from server internals

### CI: Scheduled against staging (via SSH)
Load tests run via `workflow_dispatch` or daily schedule cron — NOT on every push.

Staging details (from `cd.yml` + `docker-compose.staging.yml`):
- Staging uses HTTP (no TLS), `Caddyfile.staging`, accessed as `http://127.0.0.1:18080` from VPS
- QUIC staging port: `127.0.0.1:19090/udp` on VPS
- Smoke tests already run via SSH: `ssh deploy-target "bash ... --host 127.0.0.1 --port 18080 --scheme http"`

Load test execution (same SSH pattern as smoke tests):
- k6 runs on VPS via SSH (install k6 Docker image or binary on VPS)
- `ssh deploy-target "k6 run --env BASE_URL=http://127.0.0.1:18080 /tmp/api-baseline.js"`
- Go QUIC harness: compile, SCP to VPS, run via SSH targeting `127.0.0.1:19090`
- No additional firewall ports needed — reuses existing SSH access pattern from `cd.yml`

---

## Workstream 6: CI Pipeline Updates

### Changes to `ci.yml`

**New job: `e2e`**
- `needs:` ALL lint + test + integration + golden jobs — only run after everything passes
- Steps: checkout → setup-node → npm ci → playwright install chromium → docker compose test up → playwright test → upload report → teardown
- Uploads `playwright-report/` as artifact on failure

Explicit needs list:
```yaml
needs:
  - rust-lint
  - rust-test
  - go-lint
  - go-unit
  - go-integration
  - golden
  - web-lint
  - web-unit
  - web-integration
  - config-lint
```

**New job: `load-test`** (conditional — runs against staging via SSH)
- `if: github.event_name == 'workflow_dispatch' || github.event_name == 'schedule'`
- Triggered by daily cron (6am UTC) or manual dispatch
- Reuses the SSH + OCI firewall pattern from `cd.yml` `deploy-staging` job
- SCP k6 scripts + compiled Go QUIC harness to VPS
- Run via SSH targeting `http://127.0.0.1:18080` (HTTP) and `127.0.0.1:19090` (QUIC)
- Tests real Caddy + Docker networking under load — not CI runner limits
- No additional firewall ports needed — only SSH access required

**Update `merge-to-main`:**
- Add `e2e` to `needs` list

**Update `notify-failure`:**
- Add `e2e` to `needs` list

**Staging Playwright in `cd.yml`** (post-deploy validation, after smoke tests)

Add to `deploy-staging` job, after the existing "Run smoke tests" step:
- SCP Playwright test files + config to VPS
- Run headless Playwright via SSH against `http://127.0.0.1:18080`
- Requires Node.js + Playwright + Chromium pre-installed on VPS (one-time setup via cloud-init or manual)
- Uses `playwright.staging.config.ts` with `baseURL: 'http://127.0.0.1:18080'`
- Shares the same spec files as CI E2E (`web/e2e/*.spec.ts`) — only config differs
- On failure: triggers rollback (same as smoke test failure)
- Uploads Playwright trace/screenshot artifacts via SCP back to runner, then `upload-artifact`

VPS prerequisites (one-time setup):
```bash
# Add to cloud-init or run manually
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo bash -
sudo apt-get install -y nodejs
npm install -g @playwright/test
npx playwright install --with-deps chromium
```

**Golden file gate hardening** (in existing `golden` job):
```yaml
- name: Verify golden files unchanged
  run: git diff --exit-code testdata/golden/
```

---

## Staging vs CI for E2E Testing

**Decision: Both CI and staging — each serves a different purpose.**

**CI E2E (docker-compose.test.yml)** — gates `merge-to-main`:
- Builds from source (current branch), runs Playwright headless against `localhost:8080`
- Catches UI regressions **before** they reach `main`
- Isolated and reproducible (`tmpfs: /data` = fresh DB every run)
- No Caddy/TLS — tests the app directly

**Staging E2E (cd.yml, post-deploy)** — validates real deployment:
- Runs Playwright headless on VPS via SSH against `http://127.0.0.1:18080`
- Tests real Caddy routing, Docker networking, SPA fallback, static asset serving
- Same spec files as CI, different config (`playwright.staging.config.ts`)
- On failure: triggers rollback (same as smoke test failure)
- Catches infra-specific issues that CI E2E can't detect

---

## Makefile Additions

```makefile
e2e:
	cd deploy && docker compose -f docker-compose.test.yml up -d --build --wait
	cd web && npx playwright test
	cd deploy && docker compose -f docker-compose.test.yml down -v

# Load tests default to local; CI overrides to staging via SSH
load-test:
	k6 run --env BASE_URL=http://localhost:8080 load/k6/scenarios/api-baseline.js
	k6 run --env BASE_URL=http://localhost:8080 load/k6/scenarios/relay-throughput.js

load-test-quic:
	cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090
```

---

## Implementation Order

| Step | Workstream | Depends On | Deliverables |
|------|-----------|------------|-------------|
| 1 | WS1: Fix flaky test | — | Fixed `ConcurrentSessions` test |
| 2 | WS2: Go integration tests | WS1 | 6 new test files, ~30 tests, testutil helpers |
| 3 | WS3: Docker Compose test env | — | `docker-compose.test.yml`, `wait-for-server.sh` |
| 4 | WS4: Playwright E2E | WS3 | 4 spec files, ~15 tests, config, helpers |
| 5 | WS6: CI pipeline | WS4 | `e2e` + `load-test` jobs, gate updates |
| 6 | WS5: Load testing (k6 + Go harness) | WS3 | 3 k6 scenarios + Go QUIC harness, Makefile targets |

**Parallelizable:** WS1 + WS3 can start immediately. WS2 depends on WS1 only.

---

## Test Count Summary

| Category | Before | After |
|----------|--------|-------|
| Go integration tests | 13 | ~43 |
| Playwright E2E tests | 0 | ~15 |
| Load test scenarios | 0 | 3 k6 + 1 Go QUIC harness |
| Golden files | 10 | 10 (hardened gate) |
| **Total new tests** | — | **~50** |

---

## New Files Summary

| Path | Purpose |
|------|---------|
| `server/tests/integration/reconnect_test.go` | Agent reconnection tests |
| `server/tests/integration/relay_data_test.go` | Relay data integrity tests |
| `server/tests/integration/auth_edge_test.go` | Auth edge case tests |
| `server/tests/integration/admin_test.go` | Admin + audit log tests |
| `server/tests/integration/amt_test.go` | AMT API tests |
| `server/tests/integration/signaling_test.go` | WebRTC signaling tests |
| `deploy/docker-compose.test.yml` | Test compose environment |
| `deploy/scripts/wait-for-server.sh` | Health check poller |
| `web/playwright.config.ts` | Playwright CI config |
| `web/playwright.staging.config.ts` | Playwright staging config |
| `web/e2e/fixtures.ts` | Custom test fixtures |
| `web/e2e/helpers/api-helper.ts` | API helper for data setup |
| `web/e2e/helpers/auth-helper.ts` | Auth shortcuts |
| `web/e2e/auth.spec.ts` | Auth E2E tests |
| `web/e2e/device-list.spec.ts` | Device list E2E (empty states) |
| `web/e2e/admin.spec.ts` | Admin panel E2E tests |
| `web/e2e/navigation.spec.ts` | SPA routing, auth guard, 404 |
| `load/k6/scenarios/api-baseline.js` | API load test |
| `load/k6/scenarios/concurrent-agents.js` | Agent concurrency load test |
| `load/k6/scenarios/relay-throughput.js` | Relay throughput load test |
| `server/tests/loadtest/main.go` | Go QUIC agent load harness |

---

## Verification

1. **Flaky test fix:** `go test -race -count=50 -run ConcurrentSessions ./tests/integration/`
2. **Go integration tests:** `make test-integration` — all ~43 pass
3. **Docker Compose test env:** `cd deploy && docker compose -f docker-compose.test.yml up --build --wait` — server healthy within 30s
4. **Playwright E2E:** `make e2e` — all ~15 specs pass
5. **Load tests:** `make load-test` — all thresholds met
6. **CI pipeline:** Push to `dev`, verify `e2e` job passes and `merge-to-main` includes it in gates
7. **Golden gate:** Modify a golden file, push — verify CI fails
8. **Run `/precommit`** before every commit
9. **Run `/refactor`** after all pre-commit checks pass
