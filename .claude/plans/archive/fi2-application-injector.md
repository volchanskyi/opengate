# Micro-Plan FI2 — Go test-harness fault suite

**Master:** `context-driven-fault-injection.md` §11 (FI2), §4, §5a, §6, §10, §13.
**Branch:** `dev`. **Owner:** engineer (Go). **Sequence:** after FI1. **Depends on:** FI0 (`AgentControl` seam) and FI1 (spec + ADR-041).
**Status:** Ready after FI1. Multiple commits; keep the gauntlet green per commit.

## Goal

Prove the server's **internal** failure-handling behavior with faults injected by
**adapter substitution** in Go tests around an in-process server — **no
fault-injection code in the shipped binary** (Mechanism C, master Decisions 2 &
7). This is the reliability **gate**: it runs in `make test` / normal CI. Deployed
and network faults are **not** here — they are Chaos Mesh (FI5).

## Context (verified)

- The server is wired from consumer ports in `ServerConfig`
  ([`api.go:71-109`](../../../server/internal/api/api.go#L71-L109)): `Sessions`
  (`session.Repository`), `Devices` (`device.Repository`), `Notifier`, `AMT`
  (`amt.Operator`), `Relay`, and the FI0 `AgentControl` via `Agents`
  (`AgentGetter`). A test wires the real `Server` and **substitutes a
  fault-decorating implementation** of a chosen port — domain packages stay
  unaware and nothing ships.
- The chi stack is exercised **for real** in-process: `Recoverer` at the top
  ([`api.go:275`](../../../server/internal/api/api.go#L275)) so an injected `panic`
  returns 500; `RequestTimeout(30s)` + `RateLimiter` only inside the API group
  ([`api.go:311-312`](../../../server/internal/api/api.go#L311-L312)); WS routes
  ([`api.go:328`](../../../server/internal/api/api.go#L328)) sit **outside**
  `RequestTimeout` (not a `Hijacker`,
  [`middleware.go:37-42`](../../../server/internal/api/middleware.go#L37-L42)).
- **Existing idiom to extend:** the **port-substitution** precedent is
  `server/internal/api/store_failure_test.go` (`TestHandlerStoreFailures`), which
  substitutes a failing repository. The wire-level tests
  (`server/tests/integration/control_stream_faults_test.go`, `relay_faults_test.go`)
  inject at the **transport** level (corrupt frames / `Close()`) — reuse them for
  the connection-close / relay-drop cases, not for the port-decorator cases.
- **Tenancy (post-WS-0):** wrapped repositories run in a tenant-scoped tx. A fault
  decorator **must thread `context.Context` through unchanged** (the tenant GUC
  rides it via `dbtx`); a fault must never drop or cross the tenant context.

## File inventory

**Create**
- `server/internal/faulttest/` — a **test-support package** (fault-decorating
  implementations of the consumer ports + the action executors). It is imported
  **only** from `_test.go` files, so it is never reachable from `main` and never
  ships. A `go-arch-lint` rule asserts `cmd/meshserver` (and any prod package)
  **never** imports `faulttest` — the "no fault code in the binary" guarantee.
  - `ports.go` — `FaultRepo` (wraps `session.Repository` / `device.Repository`),
    `FaultAgentControl` (wraps the FI0 `AgentControl`), and `FaultRegistry` (wraps
    the `relay.SessionRegistry` interface, injected via `relay.WithRegistry` —
    **not** a `*relay.Relay` decorator, since `ServerConfig.Relay` is a concrete
    struct; precedent `degradedRegistry` in `handlers_health_test.go`).
    `FaultNotifier` / `FaultAMT` are **optional/candidate** (no §7 scenario drives
    them). Each reads a per-call fault spec and delegates unchanged otherwise.
  - `actions.go` — `delay`, `timeout`, `error`, `panic`, `blocked`,
    `connection-close` executors, all context-aware (exit on `ctx.Done()`).
  - `spec.go` — a typed, immutable fault spec + a test-scoped selector (which
    call/port to fault). No headers, no env, no HTTP — pure test wiring.

**Modify**
- `server/tests/integration/…` — new suite files that stand up the real `Server`
  with one substituted fault port and assert HTTP/behavioral outcomes.
- (No changes to `api.go` composition root, `main.go`, Helm, or any prod package.)

**Tests (write first — TDD)** — under `server/tests/integration/` (+ unit tests in
`faulttest`):
- `delay`/`timeout` on `device.Repository` → handler returns bounded
  timeout-class error; no leaked goroutine/txn.
- `error` on `session.Repository` → mapped typed error / HTTP status.
- `panic` on a repo call → `Recoverer` returns 500 **and the next request on the
  same server succeeds**.
- `blocked` (waits on ctx cancel) → request context cancellation unblocks it; the
  goroutine exits.
- `connection-close` via the FI0 seam / concrete conn → the server surfaces the
  disconnect (send errors, device → offline, no goroutine leak) and the harness's
  own reconnect path on the concrete conn activates (extends the wire-level fault
  tests). **Agent-side reconnect/backoff is proven by the FI5 CM drills, not here.**
- `api.before-handler` middleware fault → a test-only middleware injects the fault
  (it is **not** a `ServerConfig` port); assert the mapped status.
- **WebSocket handshake failure** → a bounded upgrade failure; extend the existing
  `server/tests/integration/middleware_ws_test.go`
  (`TestWebSocketUpgradeThroughFullMiddlewareStack`) with the failure case.
- **tenant-context preservation** — a faulted repository decorator still sees the
  request's `dbtx` tenant; a **cross-tenant-leak test** proves a fault never drops
  or crosses org context.
- **per-test isolation** — a faulted request/flow does not affect a concurrent
  unfaulted one within the same in-process server (replaces the parked
  in-deployment per-request criterion).
- **`faulttest` is not shipped** — a test (or `go-arch-lint`) asserts no prod
  package imports `faulttest`.

## Steps (TDD)

1. Branch current after FI1.
2. **Tests first:** a failing `delay`/`error` case backed by a `faulttest` port;
   implement `faulttest/spec.go` + `ports.go` + `actions.go` to pass.
3. Add the `panic`-recovery case (500 + next request succeeds) and the `blocked`
   ctx-cancel case.
4. Add `connection-close` (FI0 seam) + the tenant-preservation / cross-tenant-leak
   cases.
5. Add the per-test isolation case and the `faulttest`-not-imported guard
   (`go-arch-lint` rule + a test).
6. `/precommit` → commit → `/refactor` → `/precommit` → commit → push (per slice;
   gauntlet green each commit).

## Acceptance criteria (master §13)

1. **Zero fault code in the shipped binary** — `faulttest` is imported only by
   tests; a `go-arch-lint` rule + a test prove `cmd/meshserver` never imports it.
2. `panic` injection returns 500 while the next request on the same server
   succeeds.
3. A `blocked` dependency exits when the request context is canceled.
4. `delay`/`timeout`/`error` map to the bounded, typed outcomes FI1 specifies.
5. Every fault decorator **preserves the tenant context** (cross-tenant-leak test
   present and green).
6. Per-test isolation: a faulted flow does not affect a concurrent unfaulted one.
7. Full gauntlet green (incl. `go test -race`); coverage at/above floor.
8. Fault-suite assertions are **specific** (typed HTTP status / error class, not
   just "an error") so they clear the repo's `make mutate` floors.

## Reviewer checklist

- [ ] `faulttest` lives in a test-support package imported **only** by `_test.go`;
      `go-arch-lint` forbids any prod-package import of it (the no-ship guarantee).
- [ ] The real chi stack is exercised in-process (`Recoverer`, API-group timeout);
      panic → 500 → next request OK is asserted against the real server.
- [ ] Fault spec is typed + immutable; selection is pure test wiring (no headers,
      env, or HTTP selector — Model C is out of scope under Mechanism C).
- [ ] Every decorator threads the request `ctx` unchanged; tenant GUC survives;
      cross-tenant-leak test present.
- [ ] `connection-close` uses the FI0 seam / concrete conn (no `Close()` added to
      the api port); extends the wire-level `*_faults_test.go` idiom; asserts
      **server-side cleanup only** (agent reconnect is FI5's CM drills).
- [ ] `relay.registry` faulted via the `relay.SessionRegistry` interface
      (`relay.WithRegistry`), **not** a `*relay.Relay` decorator; precedent
      `degradedRegistry`.
- [ ] `api.before-handler` faulted via test middleware (it is not a `ServerConfig`
      port); WS handshake failure extends `middleware_ws_test.go`.
- [ ] Assertions are specific (typed status / error class) to clear mutation floors.
- [ ] No changes to `api.go` composition root, `main.go`, Helm, or any prod file.
- [ ] Deployed/network faults are **not** here — they are Chaos Mesh (FI5).
