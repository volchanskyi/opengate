# Agent E2E Test Plan

## Context

The agent binary is fully functional but has zero E2E test coverage. Existing E2E tests (15 Playwright tests) cover only browser-only flows: auth, device list empty states, admin, navigation. No tests exercise the full 3-tier flow: **browser → server → agent**. This plan adds E2E tests that validate agent-connected scenarios — device visibility, session lifecycle, terminal forwarding, and file browsing — integrated into the existing CI and staging pipelines.

### What's Already Tested (DO NOT DUPLICATE)

| Layer | Coverage | Examples |
|-------|----------|----------|
| Go integration (50+ tests) | Session lifecycle, relay data integrity, agent reconnect, signaling, admin | `session_test.go`, `relay_data_test.go`, `agentapi_test.go` |
| Rust integration (3 tests) | QUIC mTLS connect, handshake, session request roundtrip | `connection_test.rs` |
| Web unit (100+ tests) | WS transport, stores, components, codec, input handler | `ws-transport.test.ts`, all `*.test.tsx` |
| Existing E2E (15 tests) | Auth flows, empty states, admin access, SPA routing | `auth.spec.ts`, `navigation.spec.ts` |

### E2E Value-Add: What These Tests Uniquely Validate

These tests are the **only** tests that exercise the real compiled agent binary connecting via QUIC to the real server, with a real browser creating sessions through the full stack. No existing test covers this path.

---

## Workstream 1: Server Test-Mode Seed Endpoint

**Why**: The `devices` table has `group_id TEXT NOT NULL REFERENCES groups_(id) ON DELETE CASCADE`. When a new agent connects, `server.go:169` sets `groupID = uuid.Nil`, then `conn.go:156` calls `UpsertDevice` — which fails the FK constraint. Go integration tests solve this via `testutil.SeedGroup` + `store.UpsertDevice` (direct DB). E2E needs an HTTP equivalent.

**File: [api.go](server/internal/api/api.go)** (modify `routes()`)

Add after the `r.Get("/ws/relay/{token}", ...)` line, guarded by `OPENGATE_TEST_MODE`:

```go
if os.Getenv("OPENGATE_TEST_MODE") == "true" {
    r.Post("/api/v1/test/seed-device", s.handleTestSeedDevice)
}
```

**File: [handlers_test_mode.go](server/internal/api/handlers_test_mode.go)** (new)

```go
func (s *Server) handleTestSeedDevice(w http.ResponseWriter, r *http.Request) {
    // Parse: {id: uuid, group_id: uuid, hostname: string, os: string}
    // Call s.store.UpsertDevice with status "offline"
    // Return 201 Created
}
```

**File: [handlers_test_mode_test.go](server/internal/api/handlers_test_mode_test.go)** (new)

- Test: seed device returns 201
- Test: seed device not registered when OPENGATE_TEST_MODE unset

---

## Workstream 2: Docker Infrastructure

### [Dockerfile.agent](Dockerfile.agent) (new)

```dockerfile
# Stage 1: Build
FROM rust:1.83-bookworm AS build
WORKDIR /build
COPY agent/ agent/
COPY testdata/ testdata/
RUN cd agent && cargo build --release -p mesh-agent

# Stage 2: Runtime
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates bash && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /test-fixtures/subdir /agent-data
RUN echo "hello from agent" > /test-fixtures/test-file.txt \
 && echo "another file" > /test-fixtures/another.txt \
 && echo "nested content" > /test-fixtures/subdir/nested.txt
COPY --from=build /build/agent/target/release/mesh-agent /usr/local/bin/mesh-agent
COPY deploy/scripts/agent-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
```

Uses `debian:bookworm-slim` (not Alpine) — PTY allocation via `portable-pty` requires glibc.

### [deploy/scripts/agent-entrypoint.sh](deploy/scripts/agent-entrypoint.sh) (new)

```bash
#!/bin/bash
set -euo pipefail
# Write deterministic device ID
if [ -n "${OPENGATE_DEVICE_ID:-}" ]; then
    echo "$OPENGATE_DEVICE_ID" > "$OPENGATE_DATA_DIR/device_id.txt"
fi
# Wait for server CA cert (generated on server startup)
timeout 30 bash -c 'until [ -f "$OPENGATE_SERVER_CA" ]; do sleep 1; done'
exec mesh-agent \
    --server-addr "$OPENGATE_SERVER_ADDR" \
    --server-ca "$OPENGATE_SERVER_CA" \
    --data-dir "$OPENGATE_DATA_DIR"
```

### [deploy/docker-compose.test.yml](deploy/docker-compose.test.yml) (modify)

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
    command: ["-listen", ":8080", "-quic-listen", ":9090", "-data-dir", "/data", "-web-dir", "/srv/web"]
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/api/v1/health"]
      interval: 5s
      timeout: 3s
      retries: 10
      start_period: 10s
    volumes:
      - server-data:/data   # <-- was tmpfs

  agent:
    build:
      context: ..
      dockerfile: Dockerfile.agent
    container_name: opengate-test-agent
    environment:
      - OPENGATE_SERVER_ADDR=server:9090
      - OPENGATE_SERVER_CA=/data/ca.pem
      - OPENGATE_DATA_DIR=/agent-data
      - OPENGATE_DEVICE_ID=e2e00000-0000-0000-0000-000000000001
      - RUST_LOG=info
    depends_on:
      server:
        condition: service_healthy
    volumes:
      - server-data:/data:ro

volumes:
  server-data:
```

Key changes: `tmpfs` → named volume `server-data` for CA cert sharing. Agent depends on server health.

---

## Workstream 3: Playwright Helpers & Fixtures

### [web/e2e/helpers/agent-helper.ts](web/e2e/helpers/agent-helper.ts) (new)

```typescript
export const AGENT_DEVICE_ID = 'e2e00000-0000-0000-0000-000000000001';

export async function seedDevice(request, token, deviceId, groupId, hostname): Promise<void>
// POST /api/v1/test/seed-device

export async function waitForOnlineDevice(request, token, groupId, timeoutMs = 30000): Promise<Device>
// Polls GET /api/v1/devices?group_id= every 2s until status=online

export async function createSessionViaAPI(request, token, deviceId): Promise<{token, relay_url}>
// POST /api/v1/sessions
```

### [web/e2e/fixtures-agent.ts](web/e2e/fixtures-agent.ts) (new)

Extends base test with `agentEnv` fixture:
1. Creates test user (reuses `createTestUser`)
2. Creates group (reuses `createGroup`)
3. Seeds device into group with known ID
4. Waits for device to come online (polls API)
5. Provides `{token, groupId, deviceId}` to tests

Also provides `agentAuthedPage` — page with JWT injected.

---

## Workstream 4: E2E Test Specs (10 tests)

### [web/e2e/agent-device.spec.ts](web/e2e/agent-device.spec.ts) (2 tests)

| Test | What it validates |
|------|-------------------|
| Device appears online in device list | Full QUIC connect → handshake → register → UpsertDevice → API list → UI render |
| Device detail shows correct info | Hostname, OS, online status badge on detail page |

### [web/e2e/agent-session.spec.ts](web/e2e/agent-session.spec.ts) (3 tests)

| Test | What it validates |
|------|-------------------|
| Can create session from device detail | UI button → API → agent receives SessionRequest → relay established → session view renders |
| Session shows connected state | Connection indicator, tab bar (Desktop/Terminal/Files/Chat) |
| Can disconnect session | Disconnect button → cleanup → redirect to /devices |

### [web/e2e/agent-terminal.spec.ts](web/e2e/agent-terminal.spec.ts) (2 tests)

| Test | What it validates |
|------|-------------------|
| Terminal tab renders terminal element | Session → Terminal tab → xterm.js container visible |
| Echo command produces output | Type `echo MARKER_xxxxx` → see marker in output. Full path: keyboard → WS → relay → agent PTY → stdout → relay → WS → xterm |

**Determinism**: `echo UNIQUE_MARKER` is universally reliable. Unique marker per run. 10s timeout (sub-second expected).

### [web/e2e/agent-files.spec.ts](web/e2e/agent-files.spec.ts) (3 tests)

| Test | What it validates |
|------|-------------------|
| Shows directory listing | Auto-request on mount → agent FileOpsHandler → relay → UI table rows |
| Navigate into subdirectory | Click dir → new listing request → path updates, entries change |
| File metadata displayed | Size column, modified timestamp, dir shows '-' |

**Determinism**: `/test-fixtures/` baked into Docker image with known files.

---

## Workstream 5: FileManagerView Auto-Load

**File: [FileManagerView.tsx](web/src/features/file-manager/FileManagerView.tsx)** (modify)

Add `useEffect` to auto-request root directory when connected:

```typescript
useEffect(() => {
  if (connectionState === 'connected') {
    requestDirectory('/');
  }
}, [connectionState, requestDirectory]);
```

Currently the file tab renders an empty table with no way to trigger the initial listing. This is both a product gap and a testing impediment.

---

## Workstream 6: Config & CI

### [web/playwright.staging.config.ts](web/playwright.staging.config.ts) (modify)

Add `testIgnore: ['**/agent-*.spec.ts']` — agent tests require the Docker agent container, which doesn't exist in staging (production-like deploy).

### [.github/workflows/ci.yml](.github/workflows/ci.yml) (modify e2e job)

- Increase `wait-for-server.sh` timeout from 60s → 120s (Rust Docker build is slower on first run)
- Add Docker BuildKit cache for cargo registry to speed up Rust builds:
  ```yaml
  - uses: docker/setup-buildx-action@v3
  - name: Build & start test env
    run: COMPOSE_DOCKER_CLI_BUILD=1 DOCKER_BUILDKIT=1 docker compose -f docker-compose.test.yml up -d --build --wait
  ```
- No other changes needed — `docker compose up --build --wait` automatically builds + starts both services

---

## Implementation Order

1. **W1**: Server test-mode endpoint (unblocks W3, W4)
2. **W5**: FileManagerView auto-load (independent, parallel with W2)
3. **W2**: Docker infrastructure (Dockerfile.agent, compose, entrypoint)
4. **W3**: Playwright helpers and fixtures (depends on W1)
5. **W4**: E2E test specs (depends on W2, W3, W5)
6. **W6**: Config & CI updates (depends on W4)

---

## Stability Strategies

1. **Startup ordering**: `depends_on: condition: service_healthy` + entrypoint waits for CA cert file
2. **Deterministic device ID**: Fixed UUID `e2e00000-...` written to `device_id.txt` before agent starts
3. **Poll-based waits**: `waitForOnlineDevice` polls API every 2s (no hardcoded sleeps)
4. **Terminal determinism**: `echo MARKER` — universally available, unique per test run
5. **File determinism**: Known fixtures baked into Docker image
6. **Session creation**: API-first (create via REST, then navigate) avoids UI timing issues
7. **Single worker**: Tests share one agent, run serially (Playwright default)

---

## Verification

1. Build and run locally:
   ```bash
   cd deploy && docker compose -f docker-compose.test.yml up -d --build --wait
   cd ../web && npx playwright test
   docker compose -f docker-compose.test.yml down -v
   ```
2. Verify all 25 tests pass (15 existing + 10 new)
3. Push to `dev`, verify CI `e2e` job passes
4. Verify staging E2E still passes (agent tests excluded via `testIgnore`)

---

## Risks

| Risk | Mitigation |
|------|------------|
| Rust Docker build adds ~5 min to CI | BuildKit cache mounts for cargo registry/target |
| PTY fails in container | Use `debian:bookworm-slim` (glibc), not Alpine |
| Agent QUIC handshake timing | Entrypoint waits for CA cert, compose ordering via healthcheck |
| FK constraint on agent register | Test-mode seed endpoint pre-creates device in valid group |
| WebSocket relay timing | Generous 10s timeouts, poll-based waits |
