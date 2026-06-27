# Micro-Plan FI0 — `AgentControl` interface seam

**Master:** `context-driven-fault-injection.md` §11 (FI0), §6, Decision 6.
**Branch:** `dev`. **Owner:** engineer (Go). **Sequence:** first; blocks the `agent.control-write` fault point in FI2. **Depends on:** nothing (prerequisite teardown is complete).
**Status:** Ready. Pure decoupling refactor — **no fault-injection code in this micro-plan.**

## Goal

Decouple the API handlers from the concrete `*agentapi.AgentConn` so they depend
on a consumer-defined port. Net-positive decoupling on its own; it makes the
`agent.control-write` boundary wrappable by a fault decorator later (FI2).

## Context (verified against source)

`AgentGetter` returns the concrete type today:

- [`internal/api/api.go:36-39`](../../server/internal/api/api.go#L36-L39) —
  `GetAgent(db.DeviceID) *agentapi.AgentConn` and
  `ListConnectedAgents() []*agentapi.AgentConn`.

The API handlers use **two** surfaces of `AgentConn`:

1. **Control-write methods** (the 5 the api package calls):
   `SendSessionRequest`, `SendAgentUpdate`, `SendRestartAgent`,
   `SendRequestHardwareReport`, `SendRequestDeviceLogs`
   ([`conn.go`](../../server/internal/agentapi/conn.go); call sites in
   [`handlers_sessions.go:74`](../../server/internal/api/handlers_sessions.go#L74),
   [`handlers_updates.go:97`](../../server/internal/api/handlers_updates.go#L97),
   [`handlers_devices.go:35,66,129`](../../server/internal/api/handlers_devices.go#L35)).
   `SendAgentDeregistered` is **not** in scope — it is called inside `agentapi`
   itself ([`server.go:107`](../../server/internal/agentapi/server.go#L107)),
   not via the api package's `AgentGetter`.
2. **Metadata reads** — `eligibleAgents`
   ([`handlers_updates.go:126-135`](../../server/internal/api/handlers_updates.go#L126-L135))
   reads the **public fields** `DeviceID`, `OS`, `Arch`, `AgentVersion`
   ([`conn.go:19-29`](../../server/internal/agentapi/conn.go#L19-L29)).

### Design decision (must be made first — fields can't live behind an interface)

A Go interface exposes methods, not fields, and a method cannot share a name
with a field. So exposing the metadata through the port requires a shape choice:

- **(Recommended) Two narrow ports + a value accessor.** Define `AgentControl`
  with the 5 `Send*` methods **and** one `Meta() AgentMeta` accessor returning a
  small value struct `{DeviceID, OS, Arch, AgentVersion}`. Add `Meta()` to
  `AgentConn` (no field rename — `Meta` doesn't collide). `eligibleAgents` reads
  `a.Meta().OS` etc. Keeps the fault target (`Send*`) and the read model on one
  small port without touching the existing public fields or their other readers.
- *(Alt A)* Make the metadata fields unexported and add same-purpose accessor
  methods — wider blast radius (every other reader of those fields changes).
- *(Alt B)* Scope `AgentControl` to `Send*` only and keep `eligibleAgents` on the
  concrete type via a separate concrete-typed lister — leaves a concrete
  dependency in the api package; rejected (defeats the seam's purpose).

Pick **Recommended** unless review surfaces an external reader of the fields that
makes `Meta()` awkward.

## File inventory

**Create**
- (none — the interface lives in the consuming `api` package, Go idiom)

**Modify**
- `server/internal/api/api.go` — add `AgentControl` (+ `AgentMeta`) interface;
  change `AgentGetter` to `GetAgent(db.DeviceID) AgentControl` /
  `ListConnectedAgents() []AgentControl`.
- `server/internal/agentapi/conn.go` — add `Meta() AgentMeta` (or chosen
  accessor) so `*AgentConn` satisfies `AgentControl`.
- `server/internal/agentapi/server.go` — `GetAgent`/`ListConnectedAgents` return
  types change to satisfy the new interface (return the concrete `*AgentConn`,
  which now implements `AgentControl`).
- `server/internal/api/handlers_updates.go` — `eligibleAgents` reads via
  `Meta()`; its return type becomes `[]AgentControl`.
- `server/internal/api/handlers_devices.go`, `handlers_sessions.go` — adjust to
  the interface type (call sites already only call `Send*`, so minimal).

**Tests (write first — TDD)**
- `server/internal/api/api_test.go` (or a focused `agentcontrol_test.go`) — a
  compile-time assertion `var _ AgentControl = (*agentapi.AgentConn)(nil)`; a
  fake `AgentControl` used by a handler test proving handlers no longer need the
  concrete type; positive (send succeeds) and negative (send returns error →
  handler maps it) cases for at least one `Send*` path and the `eligibleAgents`
  metadata filter.

## Steps (TDD)

1. `git checkout dev && git pull --rebase origin dev`.
2. **Test first:** add the interface-satisfaction assertion + a handler test
   backed by a fake `AgentControl` (this also unblocks the TDD gate for the
   branch). Run — it fails to compile (interface doesn't exist yet).
3. Add `AgentControl` + `AgentMeta` to `api.go`; repoint `AgentGetter`.
4. Add `Meta()` to `AgentConn`; update `agentapi/server.go` getter signatures.
5. Repoint `eligibleAgents` and the three handler files to the interface.
6. `go build ./...` then `go test ./internal/api/... ./internal/agentapi/...` green.
7. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Acceptance criteria

1. No `api` package file references `*agentapi.AgentConn` directly (grep clean).
2. `*agentapi.AgentConn` satisfies `AgentControl` (compile-time assertion).
3. Handlers tested against a fake `AgentControl` (no real connection needed).
4. `eligibleAgents` filtering behavior unchanged (test asserts os/arch/version).
5. No fault-injection code introduced.
6. Full gauntlet green.

## Reviewer checklist

- [ ] Interface defined in the **consumer** (`api`) package, not `agentapi`.
- [ ] `AgentControl` surface is exactly the 5 `Send*` methods the api package
      uses (+ `Meta()`); `SendAgentDeregistered` correctly excluded.
- [ ] Metadata-access decision implemented as chosen; no field/method name
      collision; no unrelated readers of `OS/Arch/AgentVersion/DeviceID` broken.
- [ ] `go-arch-lint check` still green (no new cross-boundary dependency).
- [ ] Tests cover positive + negative `Send*` and the `eligibleAgents` filter.
- [ ] No behavior change — pure decoupling; `make golden`/e2e unaffected.
