# Test Coverage — Phase B: Coverage Depth

## Context

Builds on [Phase A](tests-coverage-phase-a-targeted-gaps.md), which closes byte-level and edge-case gaps in golden files and adds two small E2E specs. Phase B addresses larger structural gaps that Phase A does not:

1. **Core user flows without browser-level coverage** — session creation + terminal I/O, and File Manager view/download. Both are shipping product features; the current E2E suite covers auth, admin guards, routing, device list, and security-permissions, but not the core session loop.
2. **Coverage exclusions that hide regressions** — [`ci.yml:190`](../../.github/workflows/ci.yml#L190) excludes `mps/wsman` from the Go coverage gate, and `sonar-project.properties` does the same for Sonar. A regression in WSMAN protocol code silently passes both gates. The tech-debt register lists this as "SonarCloud Coverage Exclusions Need Integration Tests" in [`techdebt.md`](../techdebt.md).
3. **Postgres-native behavior** — after Phase 13a migrated SQLite → PostgreSQL 17, driver-specific behavior (`TIMESTAMPTZ` timezone handling, `JSONB` round-trip on `device_hardware`, `UUID` parsing) is not targeted by any test. A `pgx/v5` upgrade could silently change semantics.
4. **Control-stream fault injection** — handlers covering restart/hardware/logs assume the agent's control stream behaves. No test exercises mid-write close, partial frame, or handler state recovery.

Phase B expects Phase A has landed. It should be estimated as ~4–6 days of focused work.

## Approach

### 5. Session + terminal E2E

Create `web/e2e/session-terminal.spec.ts`. This is the single highest-value E2E test the suite lacks — it exercises the core product loop (session create → relay handshake → WebSocket attach → terminal I/O).

Use the existing docker-compose.test.yml stack:
- Authed page navigates to a seeded online device.
- Click "New Session", wait for session URL.
- Assert terminal prompt visible (use role selector, not brittle CSS).
- Type a command (`echo hello`), assert output contains `hello`.
- Close session, assert return to device view.

Edge cases worth covering in the same spec file:
- Session against an offline device → expected error UI.
- Mid-session agent disconnect (kill agent container from the test stack if feasible, else skip) → reconnect indicator visible.

### 6. File Manager E2E

Create `web/e2e/file-manager.spec.ts`. Covers Phase-15 feature with no browser test today.

- Navigate to a session, open Files tab.
- Assert directory listing populated.
- Click a file, assert download triggered; intercept download, byte-match the content.
- Navigate into subdirectory, then breadcrumb back.
- Permission-denied path returns visible error.

### 7. MPS / WSMAN integration test

Create `server/tests/integration/mps_wsman_test.go`. Stand up a mock AMT endpoint that speaks CIRA + WSMAN; exercise `client.go`, `operations.go`, `digest.go` happy + error paths.

Scenarios:
- Successful CIRA handshake → WSMAN Enumerate → Power actions.
- Malformed XML response → client returns structured error.
- Digest auth challenge mismatch → client retries with correct nonce.
- TLS cert validation failure → connection rejected.
- AMT channel close mid-response → client recovers.

Once coverage exceeds ~50% on the previously-excluded files, **remove the exclusions** from both gates in a follow-up commit within the same PR:
- `grep -v -E '/(testutil|metrics|mps/wsman)/|api/openapi_gen\.go'` → `grep -v -E '/(testutil|metrics)/|api/openapi_gen\.go'` in [`.github/workflows/ci.yml:190`](../../.github/workflows/ci.yml#L190)
- Shrink `sonar.coverage.exclusions` in `sonar-project.properties`

This resolves the tech-debt item in-flight.

### 8. Postgres-native integration tests

Create `server/tests/integration/postgres_native_test.go`. Uses the same `POSTGRES_TEST_URL` + schema isolation as the existing integration tests.

- `TIMESTAMPTZ` — insert with `+05:00`, read back, assert UTC normalization matches expectation.
- `JSONB` on `device_hardware.network_interfaces` — round-trip a nested array with edge-case values (null, empty string, unicode), assert bit-level fidelity.
- `UUID` — reject malformed hex string, reject too-short/too-long, accept all-lowercase + all-uppercase + mixed.
- Concurrent writes under `SERIALIZABLE` isolation if any code path uses it.

Protects against silent pgx/v5 behavioral changes.

### 9. Control-stream fault injection

Extend the existing fake-agent harness in [`server/tests/integration/agentapi_test.go`](../../server/tests/integration/agentapi_test.go) and [`relay_data_test.go`](../../server/tests/integration/relay_data_test.go). Add a "close mid-write" hook to the fake-agent helper, then create `server/tests/integration/control_stream_faults_test.go`.

Cases:
- Handler commits DB state, then agent closes stream before control frame sent → no goroutine leak, DB state is rolled back or reconciled.
- Agent sends partial frame (length header + 10 bytes, then close) → server handles gracefully.
- Two concurrent control requests to same agent; second one races with first's response → both complete correctly without frame interleaving.
- Agent returns corrupted msgpack payload → server logs, handler returns error, connection stays up.

Uses existing `-race` flag from the `go-integration` CI job — no new infra needed.

## Critical Files

**New:**
- [`web/e2e/session-terminal.spec.ts`](../../web/e2e/session-terminal.spec.ts)
- [`web/e2e/file-manager.spec.ts`](../../web/e2e/file-manager.spec.ts)
- `server/tests/integration/mps_wsman_test.go`
- `server/tests/integration/postgres_native_test.go`
- `server/tests/integration/control_stream_faults_test.go`

**Modified (in the same PR that adds the MPS/WSMAN test):**
- [`.github/workflows/ci.yml:190`](../../.github/workflows/ci.yml#L190) — shrink coverage exclusion regex after WSMAN coverage rises
- [`sonar-project.properties`](../../sonar-project.properties) — shrink `sonar.coverage.exclusions` in lockstep
- Existing fake-agent helpers in [`server/tests/integration/agentapi_test.go`](../../server/tests/integration/agentapi_test.go) and [`relay_data_test.go`](../../server/tests/integration/relay_data_test.go) — add fault-injection hooks

**Patterns to reuse:**
- Fake agent construction in existing integration tests
- Postgres schema isolation (`search_path=opengate_test`) in [`server/internal/db/store_test.go`](../../server/internal/db/store_test.go)
- Docker-compose test stack for E2E — already wired to [`playwright.config.ts`](../../web/playwright.config.ts)
- Role-based selectors from existing Playwright specs

## Verification

1. `make test` passes locally (all new Go tests green).
2. `go test -race -count=5 ./server/tests/integration/...` — new integration tests pass 5 consecutive runs.
3. `make sonar-quick` — confirm `mps/wsman` files now appear in the coverage report **before** shrinking the exclusion list.
4. After exclusion shrink: full CI passes, overall coverage % unchanged or higher.
5. `make e2e -- --grep "session"` and `--grep "file manager"` pass against the docker-compose stack.
6. Tech-debt entry "SonarCloud Coverage Exclusions Need Integration Tests" can be partially or fully retired from [`techdebt.md`](../techdebt.md).
7. On merge, SonarCloud dashboard reflects newly covered files and higher MPS/WSMAN coverage.

## Done-When

- Session + terminal E2E green.
- File Manager E2E green.
- MPS/WSMAN integration tests green; exclusions shrunk; CI still passes.
- Postgres-native tests green.
- Control-stream fault injection tests green with `-race`.
- [`techdebt.md`](../techdebt.md) updated to reflect the resolved exclusion item.
- Phase entry added to [`phases.md`](../phases.md).

## Follow-Up (Out of Scope)

- Phase C (structural hardening): Go→Rust reverse goldens, protocol versioning, cross-browser E2E, visual regression, a11y, `t.Parallel()`. See [`tests-coverage-phase-c-structural-hardening.md`](tests-coverage-phase-c-structural-hardening.md).
