# Broad Codebase Refactoring Plan

## Context

Full-codebase refactoring pass across all three components (Rust agent, Go server, React/TS web client). Exploration revealed the codebase is already in good shape overall — no critical architecture issues. The main opportunities are: DRY reduction in repeated patterns, module organization for large files, and accessibility polish.

Each work item is independently committable. TDD mandate applies: failing tests first, then implement, then `/precommit`, then `/refactor`.

Phase 1 (localStorage JWT → httpOnly cookie) was removed after risk analysis: WebSocket relay auth breakage, E2E test rework, CSRF introduction, and dev proxy complexity outweigh the XSS mitigation benefit at this stage.

---

## Phase 2: DRY Reduction

### WI-2.1: Extract admin check helper (Go server)

`if !isAdmin(ctx)` duplicated **16 times** across 5 handler files. Since oapi-codegen strict handlers return typed response objects per-endpoint, a single middleware can't return the right type. Use a generic helper instead:

```go
func denyIfNotAdmin[T any](ctx context.Context, forbidden T) (*T, bool) {
    if !isAdmin(ctx) { return &forbidden, true }
    return nil, false
}
```

**Files:** `middleware.go` (add helper), `handlers_enrollment.go` (3), `handlers_users.go` (3), `handlers_updates.go` (3), `handlers_audit.go` (1), `handlers_security_groups.go` (6)
**Lines:** ~80 changed, net -32
**Risk:** Low

### WI-2.2: Extract Zustand async action helper (web)

9 stores repeat `isLoading`/`error` boilerplate. Create shared helper:

```typescript
// web/src/state/create-api-action.ts
export async function apiAction<S extends { isLoading: boolean; error: string | null }>(
  set: (partial: Partial<S>) => void,
  fn: () => Promise<void>
) { ... }
```

**Files:** New `create-api-action.ts`, then refactor: `auth-store.ts`, `device-store.ts`, `admin-store.ts`, `session-store.ts`, `file-store.ts`, `push-store.ts`, `update-store.ts`, `security-groups-store.ts`
**Lines:** ~60 new + ~100 modified, net -80
**Risk:** Low

### WI-2.3: Generic scan list helper (Go server)

9 `ListX` methods in `server/internal/db/sqlite.go` repeat the rows-iteration pattern. Extract:

```go
func scanList[T any](rows *sql.Rows, scanFn func(scanner) (*T, error)) ([]*T, error)
```

**Files:** `sqlite.go`
**Lines:** ~80 changed, net -50
**Risk:** Low

### WI-2.4: Replace manual query builder with structured builder

`QueryAuditLog()` in `sqlite.go:461` uses `query += " AND ..."`. Replace with a slice-based where-clause accumulator for clarity.

**Files:** `sqlite.go`
**Lines:** ~20
**Risk:** Low

---

## Phase 3: Module Organization

### WI-3.1: Split session.rs (617 lines) into submodules

`agent/crates/mesh-agent-core/src/session.rs` handles relay, frame dispatch, terminal, file ops, WebRTC signaling.

**Split into:**
- `session/mod.rs` — `SessionHandler` struct + `run()` (~80 lines)
- `session/handler.rs` — `handle_frame()`, `handle_control()` (~120 lines)
- `session/relay.rs` — `ws_writer_loop`, `capture_loop`, `send_frame`, `build_relay_url` (~80 lines)
- `session/terminal.rs` — `TerminalHandle` (~60 lines)

**Files:** `mesh-agent-core/src/session.rs` → `session/` dir, update `lib.rs`
**Lines:** ~617 moved, net 0
**Risk:** Low — pure reorganization

### WI-3.2: Split types.rs (456 lines) into submodules

`agent/crates/mesh-protocol/src/types.rs` mixes device, frame, control, and session types.

**Split into:** `types/{mod.rs, device.rs, frame.rs, control.rs, session.rs}`

**Files:** `mesh-protocol/src/types.rs` → `types/` dir, update `lib.rs`
**Lines:** ~456 moved, net 0
**Risk:** Low

### WI-3.3: Extract test cert helpers (Rust agent)

Cert generation duplicated in `mesh-agent/tests/connection_test.rs:24-90` and `mesh-agent/src/main.rs:407-430`.

**Create:** `mesh-agent/tests/test_helpers.rs` with shared `generate_test_ca()`, `generate_test_cert()`
**Lines:** ~60 new, ~100 removed, net -40
**Risk:** Low

### WI-3.4: Split large web components

**AgentSetupPage.tsx** (383 lines) → extract `EnrollmentTokenForm.tsx`, `InstallInstructions.tsx`
**AgentUpdates.tsx** (309 lines) → extract `ManifestPublishForm.tsx`, `ManifestList.tsx`

**Lines:** ~692 reorganized, ~40 new tests
**Risk:** Low

---

## Phase 4: Polish

### WI-4.1: Use net.SplitHostPort() for port stripping

Replace manual byte iteration at `handlers_enrollment.go:130-139` with `net.SplitHostPort()`.

**Lines:** ~15 | **Risk:** Very low

### WI-4.2: Add .expect() messages to test unwraps

Replace ~50 bare `.unwrap()` with `.expect("context")` in `mesh-agent/tests/connection_test.rs`.

**Lines:** ~50 modified | **Risk:** Very low

### WI-4.3: Log silenced errors in WebRTC/WS transports

Replace `.catch(() => {})` with `.catch((e) => console.warn(...))` in `connection-store.ts`.

**Lines:** ~5 | **Risk:** Very low

### WI-4.4: Add aria-labels to icon buttons

Audit all `<button>` with icon-only content, add `aria-label` for screen readers.

**Lines:** ~30-50 | **Risk:** Very low

---

## Deliberately Excluded

- **Store interface splitting** (40+ methods) — premature until PostgreSQL lands in Phase 13
- **Transaction support** — not needed until multi-statement ops in Phase 13
- **React.memo()** — app too small to benefit; premature optimization
- **Platform factory deduplication** (Rust) — same signatures, different implementations = idiomatic, not duplication

---

## Verification

After each work item:
1. `/precommit` — all lints, tests, benchmarks pass
2. `/refactor` — post-commit cleanup
3. E2E: `make e2e` passes

After all phases:
- `make build && make test && make lint` — clean
- Full E2E suite passes locally and in CI
- `git log --oneline` shows 12 clean, focused commits
