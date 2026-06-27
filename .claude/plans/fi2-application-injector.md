# Micro-Plan FI2 — Application injector (Option B)

**Master:** `context-driven-fault-injection.md` §11 (FI2), §4, §5, §6, §10, §13.
**Branch:** `dev`. **Owner:** engineer (Go). **Sequence:** after FI1. **Depends on:** FI0 (`AgentControl` seam — for `agent.control-write`) and FI1 (spec + mechanism choice + ADR-041).
**Status:** Ready after FI1. **The bulk of the effort.** Multiple commits; keep the gauntlet green per commit.

## Goal

Build the compiled-in, typed, context-driven fault injector — **inert unless
Helm sets the staging-only enable flag** — wired as port decorators at the
composition root, with API/WS selection middleware, metrics, logs, and a proof
of zero overhead when disabled.

## Context (verified)

- chi stack: `Recoverer` at top
  ([`api.go:233`](../../server/internal/api/api.go#L233)); `RequestTimeout(30s)`
  + `RateLimiter` apply **only** inside the API group
  ([`api.go:268-270`](../../server/internal/api/api.go#L268-L270)). WS routes
  (`/ws/relay/{token}`, [`api.go:286`](../../server/internal/api/api.go#L286))
  sit **outside** `RequestTimeout` (it is not a `Hijacker` —
  [`middleware.go:17-24`](../../server/internal/api/middleware.go#L17-L24)).
  ⟹ inject the API selector **after** `RequestTimeout` in the API group, and a
  **separate** selector at the WS route.
- Ports to wrap live in `ServerConfig`
  ([`api.go:51-76`](../../server/internal/api/api.go#L51-L76)): `Sessions`
  (`session.Repository`), `Devices` (`device.Repository`), `Relay`, `Notifier`,
  `AMT` (`amt.Operator`), and the FI0 `AgentControl` for `agent.control-write`.
- Decorators wrap ports at the **composition root** — domain packages stay
  unaware of fault injection (master §4).

## File inventory

**Create**
- `server/internal/faultinject/config.go` — typed immutable `Spec` (profile,
  fault point, action, params); profile resolution from startup config.
- `server/internal/faultinject/context.go` — store/read the typed `Spec` in
  `context.Context` (no string maps).
- `server/internal/faultinject/middleware.go` — API selection middleware (after
  `RequestTimeout`) + a separate WS-route selector; token validation; Model C
  path binding; fail-closed branches.
- `server/internal/faultinject/actions.go` — `delay`, `timeout`, `error`,
  `panic`, `blocked`, `connection-close` executors (all context-aware).
- `server/internal/faultinject/decorators.go` — port decorators for the gating
  core (`session.Repository`, `device.Repository`) + `api.before-handler`, plus
  `notifications.dispatch`, `amt.operator`, `agent.control-write`,
  `relay.registry`. `relay.session-drop` stays **deferred** (master §6).
- `server/internal/faultinject/metrics.go` — scenario/fault-point/action/result
  labels, reusing the existing prometheus registry.

**Modify**
- `server/internal/api/api.go` — at the composition root, wrap configured ports
  with decorators **only when** injection is enabled; add the two selectors.
- `server/cmd/meshserver/...` — read the enable flag/profiles from env (set by
  Helm in FI3); default disabled.

**Tests (write first — TDD)**
- `server/internal/faultinject/*_test.go`:
  disabled behavior (no decorator effect, no goroutine/timer — assert via the
  FI1 benchmark + a goroutine-count guard); token absent/invalid → fail-closed;
  unknown profile → fail-closed; fault headers on the public path rejected
  (Model C); `delay`/`timeout` exit on ctx cancel; `blocked` exits on ctx
  cancel; `panic` → `Recoverer` returns 500 **and the next request succeeds**;
  `error` maps to the configured typed error / HTTP status;
  **concurrent-request isolation** (a faulted request does not affect an
  unselected concurrent one); `connection-close` closes the selected
  agent/relay conn and the reconnect path activates.

## Steps (TDD)

1. Branch current after FI1.
2. **Tests first** for the disabled path + token/profile fail-closed; implement
   `config.go`/`context.go`/`middleware.go` to pass.
3. Add action executors with their cancellation tests.
4. Add decorators for the gating core; wire at the composition root behind the
   enable flag; isolation + panic-recovery tests.
5. Extend to `notifications`, `amt`, `agent.control-write`, `relay.registry`.
6. Metrics + structured logs (scenario ID label).
7. Re-run the FI1 disabled-overhead benchmark; assert no regression.
8. `/precommit` → commit → `/refactor` → `/precommit` → commit → push (repeat per
   logical slice; keep gauntlet green each commit).

## Acceptance criteria (master §13)

1. Normal/disabled deployment: zero overhead, no goroutine/timer on the disabled
   path (benchmark + goroutine-count assertion).
2. A selected request triggers a bounded internal fault **without** affecting an
   unselected concurrent request.
3. Panic injection returns 500 while the next request succeeds.
4. A blocked dependency exits when the request context is canceled.
5. Fail-closed: outside staging, unknown profile, missing/invalid token, or fault
   headers on the public Ingress path.
6. Decorators wrap only at the composition root; domain packages unchanged.
7. Full gauntlet green; coverage at/above floor.

## Reviewer checklist

- [ ] API selector sits **after** `RequestTimeout`; WS has its **own** selector.
- [ ] Disabled path proven inert (benchmark + goroutine guard), not assumed.
- [ ] `Spec` is typed + immutable in context; no arbitrary duration/status/module
      strings accepted from the request.
- [ ] Model C: fault headers honored only on the cluster-internal path.
- [ ] Concurrent-isolation test present and meaningful.
- [ ] No fault logic leaks into domain packages; `go-arch-lint` green.
- [ ] No secret/token logged; token comparison is constant-time.
- [ ] `relay.session-drop` left deferred per spec; no deleted fault points wired.
