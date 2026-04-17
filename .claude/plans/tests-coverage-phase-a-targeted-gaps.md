# Test Coverage — Phase A: Targeted Gap-Closers

## Context

OpenGate's CI pipeline already enforces substantial test coverage ([`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)):
- Go unit coverage ≥ 80% (hard fail, line 185–199)
- Go integration tests run with `-race` (line 207–240)
- Web coverage ≥ 80% (line 391–401)
- Rust coverage ≥ 80% (line 87–97)
- Golden-file Rust→Go verification with `git diff --exit-code` (line 311–332)
- SonarCloud quality gate blocks merge
- All the above required for `merge-to-main`

An audit of the existing suites found that negative paths, common integration flows, handler-level happy-paths, reconnect, concurrent sessions, relay bidirectional flow, and auth edge cases are already covered in `server/tests/integration/` (13 files, ~50 tests) and `server/internal/api/*_test.go`. Device logs, hardware report, restart, and updates all have both unit and integration coverage.

**What CI does not catch today:**
- Missing golden files for Rust `ControlMessage` variants that cross the Go boundary — no byte-match test exists to catch drift on those variants.
- No edge-case golden corpus (empty payload, near-max size, multi-byte UTF-8, optional-field-absent, unknown-extra-key forward-compat).
- Several user-visible product features have zero Playwright coverage (device logs UI, capability-based tab rendering).

This phase closes those gaps with the smallest possible change. Each item either extends an existing CI gate's reach or adds a Playwright spec that the existing `e2e` job picks up automatically.

## Approach

### 1. Audit Rust `ControlMessage` vs Go `ControlMessageType`, add goldens only for cross-boundary variants

Rust defines 35 `ControlMessage` variants in `agent/crates/mesh-protocol/src/control.rs`; Go decodes only 28 in `server/internal/protocol/control.go`. The delta (7 variants) are agent-local (WebRTC data-channel input: `MouseMove`, `MouseClick`, `KeyPress`, `TerminalResize`, etc.) — they never pass through Go and don't need Go goldens.

For each Rust variant that also exists in Go's `ControlMessageType` and does **not** already have a golden, add:
- Rust generator case in [`golden_test.rs`](../../agent/crates/mesh-protocol/tests/golden_test.rs) following existing `write_golden_file!` patterns.
- Go verifier case in the table at [`golden_test.go:57–104`](../../server/internal/protocol/golden_test.go#L57-L104) asserting full decoded struct fidelity (not just `err == nil`).
- Golden `.bin` file committed to `testdata/golden/`.

Expected new goldens (final list determined by the per-variant audit): `control_session_accept.bin`, `control_session_reject.bin`, `control_session_request.bin`, `control_file_list_request.bin`, `control_file_list_response.bin`, `control_file_list_error.bin`, `control_file_download_request.bin`, `control_file_upload_request.bin`, `control_chat_message.bin`, `control_update_check_response.bin`, `control_request_chat_token.bin`, `control_chat_token_response.bin`.

Once committed, the existing `golden` CI job (line 311–332) enforces byte-match on every push.

### 2. Edge-case golden corpus

Add 5 new goldens capturing edge cases not exercised by the per-variant files:
- `control_agent_register_empty_capabilities.bin` — optional-field-absent / empty collection.
- `control_hardware_report_max_size.bin` — near-16-MiB payload (bounded by [`codec.rs:6`](../../agent/crates/mesh-protocol/src/codec.rs#L6)).
- `control_agent_register_utf8.bin` — emoji + CJK in hostname field.
- `control_chat_message_forward_compat.bin` — contains an unknown msgpack key that Go must silently ignore.
- `handshake_server_hello_le_length.bin` (optional, as a negative test) — 4-byte length header in little-endian; Go decode must return an error.

Go verifiers must assert the specific semantics (emoji survives round-trip, unknown key ignored, LE header rejected).

### 3. E2E spec: Device Logs UI

Create [`web/e2e/device-logs.spec.ts`](../../web/e2e/device-logs.spec.ts). Follow the fixture pattern in [`web/e2e/admin.spec.ts`](../../web/e2e/admin.spec.ts) (authed page, API-seeded data).

Cases:
- Seed 25 log rows via API, navigate to device logs tab, assert 10 visible (pagination default).
- Click "next page", assert remaining 15 visible.
- Select level filter (info / warn / error), assert only matching rows.
- Empty state: device with no logs, assert empty-state message.

### 4. E2E spec: capability-based tabs

Create [`web/e2e/capability-tabs.spec.ts`](../../web/e2e/capability-tabs.spec.ts).

Cases:
- Seed a Linux-only-capability agent (capabilities = `["terminal", "file_manager"]`), open session view, assert "Desktop" tab hidden and "Terminal" + "Files" tabs visible.
- Seed a Windows agent with full capabilities, assert all tabs visible.
- Seed an MPS-only agent, assert appropriate subset.

Use the existing `docker-compose.test.yml` stack — no new infra.

## Critical Files

**New:**
- `testdata/golden/control_session_accept.bin` + 11 other cross-boundary goldens (final list from per-variant audit)
- `testdata/golden/control_agent_register_empty_capabilities.bin`, `control_hardware_report_max_size.bin`, `control_agent_register_utf8.bin`, `control_chat_message_forward_compat.bin`, `handshake_server_hello_le_length.bin`
- [`web/e2e/device-logs.spec.ts`](../../web/e2e/device-logs.spec.ts)
- [`web/e2e/capability-tabs.spec.ts`](../../web/e2e/capability-tabs.spec.ts)

**Modified:**
- [`agent/crates/mesh-protocol/tests/golden_test.rs`](../../agent/crates/mesh-protocol/tests/golden_test.rs) — add generator cases for each new golden
- [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go) — add verifier cases in the existing table at line 57

**Patterns to reuse:**
- Golden test table: [`golden_test.go:57–104`](../../server/internal/protocol/golden_test.go#L57-L104)
- Rust generator macros in [`golden_test.rs`](../../agent/crates/mesh-protocol/tests/golden_test.rs)
- E2E fixtures `authedPage` / `adminPage` + API helper in `web/e2e/`
- Device-seeding helper in `web/e2e/api-helper.ts` (extend if needed)

## Verification

1. `make golden` succeeds locally; `git diff --exit-code -- testdata/golden/` is clean — proves new goldens are stable.
2. `cd server && go test ./internal/protocol/ -run TestGolden -v` — all new table cases pass.
3. Open a branch that deliberately changes one byte in a Rust encoder for a newly-covered variant; push and confirm the `golden` CI job fails at `git diff --exit-code`.
4. `make e2e -- --grep "device logs"` and `--grep "capability"` both pass against the existing docker-compose.test.yml stack.
5. Full `make precommit` passes — no regressions in coverage %, Sonar, or any existing gate.
6. SonarCloud PR view shows no new issues and positive coverage delta.

## Done-When

- Every Rust `ControlMessage` variant that Go decodes has a golden file.
- 5 edge-case goldens committed and verified by Go side.
- Device Logs and capability-tabs E2E specs green in CI.
- CI's existing `golden` job fails if any of the new goldens drifts.
- Phase entry added to [`phases.md`](../phases.md) on completion.

## Follow-Up (Out of Scope)

- Phase B (coverage depth): session/terminal E2E, File Manager E2E, MPS/WSMAN integration tests, Postgres-native tests, control-stream fault injection. See [`tests-coverage-phase-b-coverage-depth.md`](tests-coverage-phase-b-coverage-depth.md).
- Phase C (structural hardening): Go→Rust reverse goldens, protocol versioning, cross-browser E2E, visual regression, a11y, `t.Parallel()`. See [`tests-coverage-phase-c-structural-hardening.md`](tests-coverage-phase-c-structural-hardening.md).
