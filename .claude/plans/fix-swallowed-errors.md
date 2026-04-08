# Fix Silently Swallowed Errors Across the Project

## Context
A whole-project audit (Rust agent, Go server, TS web client) found a small number of real bugs where errors are silently dropped, plus a larger set of "intentional best-effort" discards that have no observability. Silent failures make outages and regressions hard to diagnose — especially in the agent's session/relay paths and the web client's retry/WebRTC flows. This change fixes the real bugs, makes intentional discards observable via structured logs, and adds lint enforcement so the patterns cannot regress.

User decisions:
- **Scope:** fix bugs + add tracing on best-effort + enforce via lints.
- **TS UX:** surface user-facing failures via existing `useToastStore` (`web/src/state/toast-store.ts`).

---

## Real bugs to fix

### Go — `server/`
1. **[server/internal/agentapi/server.go:202](server/internal/agentapi/server.go#L202)** — `_ = codec.WriteFrame(...)` in tombstoned-device rejection path. Log the write error at `warn` (frame is best-effort but the failure must be visible).
2. **[server/internal/agentapi/server.go:90](server/internal/agentapi/server.go#L90)** — `_ = ac.Close()` in `DeregisterAgent`. Log Close error at `warn`.

### TypeScript — `web/src/`
3. **[web/src/state/device-store.ts:135-142](web/src/state/device-store.ts#L135-L142)** and **[device-store.ts:171-179](web/src/state/device-store.ts#L171-L179)** — `setTimeout(async () => …)` retries for hardware/logs swallow rejections. Wrap body in try/catch; on failure call `useToastStore.getState().addToast('Failed to refresh hardware', 'error')` (and equivalent for logs).
4. **[web/src/features/file-manager/use-file-manager.ts:72-76](web/src/features/file-manager/use-file-manager.ts#L72-L76)** — `blob.text().then(...)` with no `.catch`. Add `.catch` that clears the in-flight download and toasts `'Failed to read file contents'`.
5. **[web/src/state/connection-store.ts:155-160](web/src/state/connection-store.ts#L155-L160)** (and the same shape at lines 44-47, 53-56) — WebRTC `createOffer` failures only `console.warn`. Keep the state transition, also toast `'WebRTC unavailable, using WebSocket fallback'` once per session (guard with a flag in the store to avoid spam).

---

## Best-effort discards → add structured logs (no behaviour change)

### Rust — `agent/crates/`
Replace `let _ = expr;` with `if let Err(e) = expr { tracing::warn!(error = %e, "<context>"); }` at:
- [mesh-agent-core/src/session/handler.rs:123,136-137,149](agent/crates/mesh-agent-core/src/session/handler.rs#L123) — input injection failures (`target = "input"`).
- [mesh-agent-core/src/session/relay.rs:44](agent/crates/mesh-agent-core/src/session/relay.rs#L44) — ws close on loop exit (`debug!` not warn).
- [mesh-agent/src/main.rs:253](agent/crates/mesh-agent/src/main.rs#L253) — log dir creation.
- [mesh-agent/src/main.rs:791](agent/crates/mesh-agent/src/main.rs#L791) — `systemctl daemon-reload` (downgrade to `debug!` since non-systemd is expected).
- [mesh-agent/src/main.rs:834](agent/crates/mesh-agent/src/main.rs#L834) — `AgentUpdateAck` send.
- [platform-linux/src/service.rs:30](agent/crates/platform-linux/src/service.rs#L30) — sd_notify send (`debug!`).
- [mesh-agent-core/src/session/terminal_handle.rs:34,41,47](agent/crates/mesh-agent-core/src/session/terminal_handle.rs#L34) — `try_send` to stdin/resize channels. Use `trace!` (channel-closed-during-shutdown is normal) but DO log `TrySendError::Full` at `warn!` since that indicates back-pressure.
- Leave `let _guard = ENV_LOCK.lock().unwrap();` alone (not error swallowing).
- Leave `file_ops.rs:51-52` `.ok()` chains alone (semantically Option mapping for optional metadata).

### Go — `server/`
- [server/internal/mps/mps.go:228](server/internal/mps/mps.go#L228) — `SetDeadline` on close: keep the `//nolint:errcheck` (truly cleanup) but add a comment justifying it.
- [server/internal/mps/mps.go:540](server/internal/mps/mps.go#L540) — `ch.fwd.Close()`: log error at `debug` via the existing logger field on `Conn`.
- [server/internal/mps/wsman/client.go:116](server/internal/mps/wsman/client.go#L116) — already has `//nolint:errcheck` and "best effort" comment; leave as-is.

---

## Lint enforcement (prevent regression)

### Rust
Add to `[workspace.lints.clippy]` in root `Cargo.toml`:
```toml
let_underscore_must_use = "warn"
let_underscore_future = "warn"
```
Then any remaining `let _ = result;` must use `#[allow(...)]` with a justification, or be rewritten to log.

### Go
Update `.golangci.yml` (or create if absent) to enable:
- `errcheck` (already standard) with `check-blank: true` so `_ = fn()` is also flagged.
- `errorlint` for proper `errors.Is/As` use.
Existing `//nolint:errcheck` directives remain valid escape hatches.

### TypeScript
Update `web/eslint.config.*` to enable:
- `@typescript-eslint/no-floating-promises: 'error'`
- `@typescript-eslint/no-misused-promises: 'error'`
Run `npm run lint -- --fix` is unsafe here; manually fix any new flags.

---

## Critical files to modify
- `server/internal/agentapi/server.go`
- `server/internal/mps/mps.go`
- `web/src/state/device-store.ts`
- `web/src/state/connection-store.ts`
- `web/src/features/file-manager/use-file-manager.ts`
- `agent/crates/mesh-agent-core/src/session/{handler,relay,terminal_handle}.rs`
- `agent/crates/mesh-agent/src/main.rs`
- `agent/crates/platform-linux/src/service.rs`
- `Cargo.toml` (workspace lints)
- `.golangci.yml`
- `web/eslint.config.{js,ts}`

## Reused utilities
- `useToastStore.addToast` — `web/src/state/toast-store.ts:20`
- `tracing::warn!` / `debug!` / `trace!` — already used throughout the agent crates
- existing `*zap.Logger` field on `agentapi.Server` and `mps.Conn`

## TDD / verification
1. **Rust:** add unit test in `mesh-agent-core/src/session/handler.rs` that calls handler with a `NullInput` returning `Err` and asserts a `tracing` event was emitted (use `tracing-test` crate or capture via `tracing-subscriber::fmt::TestWriter`).
2. **Go:** add a test in `server/internal/agentapi/server_test.go` that forces `WriteFrame` to fail (fake stream) and asserts the rejection path logs at `warn`.
3. **TS:** add Vitest cases:
   - `device-store.test.ts` — mock `api.GET` to reject inside the retry; assert `useToastStore.getState().toasts` contains one error toast.
   - `use-file-manager.test.ts` — mock `blob.text` to reject; assert toast emitted and download cleared.
   - `connection-store.test.ts` — mock `webrtc.createOffer` to reject; assert single fallback toast and `signalingState === 'fallback'`.
4. **Lint gates:**
   - `cd agent && cargo clippy --workspace --all-targets -- -D warnings`
   - `cd server && golangci-lint run ./...`
   - `cd web && npm run lint`
5. **Full pipeline:** `/precommit` (mandatory per CLAUDE.md), then `make e2e` to confirm UI wiring.

## Out of scope
- Refactoring existing logging structure.
- Backfilling tests for code paths unrelated to error swallowing.
- Operational scripts / infra changes.
