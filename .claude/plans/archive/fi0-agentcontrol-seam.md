# Micro-Plan FI0 — `AgentControl` interface seam

**Master:** `context-driven-fault-injection.md` §11 (FI0), §6, Decisions 6 & 9.
**Branch:** `dev`. **Owner:** engineer (Go). **Sequence:** first; enables the `agent.control-write` harness scenario in FI2. **Depends on:** nothing (prerequisite teardown is complete).
**Status:** Ready. Decoupling refactor **+ a metadata concurrency fix** (0.4) — **no fault-injection code in this micro-plan.**

## Goal

Decouple the API handlers from the concrete `*agentapi.AgentConn` so they depend
on a consumer-defined port, and fix the pre-existing metadata data race. Net-
positive decoupling on its own; it lets the **Go test-harness** substitute a
fault-decorating `AgentControl` for the `agent.control-write` scenario (FI2).

## Context (verified against source)

`AgentGetter` returns the concrete type today:

- [`internal/api/api.go:42-46`](../../../server/internal/api/api.go#L42-L46) —
  `GetAgent(db.DeviceID) *agentapi.AgentConn`,
  `ListConnectedAgents() []*agentapi.AgentConn`, and
  `DeregisterAgent(ctx, db.DeviceID)`.

The API handlers use **three** surfaces of `AgentConn`:

1. **Control-write methods** (the 4 the api package calls):
   `SendSessionRequest`
   ([`handlers_sessions.go:74`](../../../server/internal/api/handlers_sessions.go#L74)),
   `SendAgentUpdate`
   ([`handlers_updates.go:97`](../../../server/internal/api/handlers_updates.go#L97)),
   `SendRestartAgent`
   ([`handlers_device_actions.go:35`](../../../server/internal/api/handlers_device_actions.go#L35)),
   and `SendRequestHardwareReport`
   ([`handlers_device_inventory.go:41`](../../../server/internal/api/handlers_device_inventory.go#L41)).
   Several are **capability-gated** — a send may return
   `ErrCapabilityNotAdvertised` (test via `IsCapabilityError`), a typed error
   class the port and its later fault decorator must preserve.
   `SendRequestDeviceLogs` is **no longer called by the api package** — it now
   sits behind `RequestLogsSync` (surface 2). `SendAgentDeregistered` is **not**
   on the api boundary either — it is called inside `agentapi`
   ([`deregister.go:48`](../../../server/internal/agentapi/deregister.go#L48)); the
   api package triggers deregistration through `AgentGetter.DeregisterAgent`
   ([`handlers_device_actions.go:118`](../../../server/internal/api/handlers_device_actions.go#L118)),
   which takes a `db.DeviceID`, not the conn.
2. **Synchronous request/response reads** (2): `RequestLogsSync`
   ([`handlers_device_inventory.go:109`](../../../server/internal/api/handlers_device_inventory.go#L109))
   and `RequestLocalHistorySync`
   ([`handlers_device_history.go:57`](../../../server/internal/api/handlers_device_history.go#L57))
   send a request and block for the agent's response, returning data. They make
   the seam broader than a pure control-write port — the design decision below
   settles whether they live on `AgentControl` or a sibling read port.
3. **Metadata reads** — `eligibleAgents`
   ([`handlers_updates.go:127-145`](../../../server/internal/api/handlers_updates.go#L127-L145))
   reads the **public fields** `DeviceID`, `OS`, `Arch`, `AgentVersion`
   ([`conn.go:25-39`](../../../server/internal/agentapi/conn.go#L25-L39), which now
   also carry `OrgID`, `GroupID`, `Capabilities` — not needed by
   `eligibleAgents`, so out of the `Meta()` value below).

### Design decision (must be made first — fields can't live behind an interface)

A Go interface exposes methods, not fields, and a method cannot share a name
with a field. So exposing the metadata through the port requires a shape choice:

- **(Recommended) One `AgentControl` port + a value accessor.** Define
  `AgentControl` with the four `Send*` control-writes, the two `Request*Sync`
  reads, **and** one `Meta() AgentMeta` accessor returning a small value struct
  `{DeviceID, OS, Arch, AgentVersion}`. Add `Meta()` to `AgentConn` (no field
  rename — `Meta` doesn't collide). `eligibleAgents` reads `a.Meta().OS` etc.
  `agent.control-write` (FI2) decorates the four `Send*`; the `Request*Sync`
  reads are a second, independently decoratable surface on the same port. Keeps
  the whole api↔agent boundary behind one small port without touching the
  existing public fields or their other readers.
- *(Alt A)* Split into `AgentControl` (`Send*` only) + `AgentQuerier`
  (`Request*Sync`) — cleaner CQS separation, but two ports and two fake types for
  a boundary that is always the same concrete conn; adopt only if FI2 wants to
  gate writes and reads under different profiles.
- *(Alt B)* Make the metadata fields unexported and add same-purpose accessor
  methods — wider blast radius (every other reader of those fields changes).
- *(Alt C)* Scope the port to `Send*` only and keep `eligibleAgents` +
  `Request*Sync` on the concrete type — leaves a concrete dependency in the api
  package; rejected (defeats the seam's purpose).

Pick **Recommended** unless review surfaces a reason to split writes from reads.

### Decisions (locked with the user, 2026-07-15)

Under the settled mechanism — **no fault code in the production binary**; internal
faults via a **Go test-harness** (adapter substitution), deployed faults via
**Chaos Mesh** (master Decisions 2 & 9). The four options were scored against
`context-driven-fault-injection.md` §6/§7:

1. **Single port (0.2).** §7 lists exactly **one** agent scenario —
   *"Agent control-write fault"* (a **write** fault) — and **no** read-fault
   scenario for the two `Request*Sync` pulls. The Go harness substitutes one
   fault-decorating `AgentControl` and faults whichever methods it chooses
   (writes only), passing reads and `Meta()` through — so a split
   (`AgentControl` + `AgentQuerier`, Alt A) buys nothing and is rejected. One
   fake, one substitution point.
2. **`AgentMeta` lives in `agentapi`, not `api`.** `Meta()` is a method on
   `*agentapi.AgentConn`, so its return type cannot be `api.AgentMeta` without an
   `agentapi → api` import cycle (`api` already imports `agentapi`). Define the
   value struct `AgentMeta{DeviceID, OS, Arch, AgentVersion}` in
   `agentapi/conn.go`; the api-package `AgentControl` interface's `Meta()` returns
   `agentapi.AgentMeta`. (Referencing a value type is fine — the seam only bans a
   dependency on the concrete `*agentapi.AgentConn`.)
3. **`*agentapi.AgentServer` cannot directly satisfy the new `AgentGetter`.** Once
   `AgentGetter.GetAgent` returns `api.AgentControl`, `*AgentServer` can't satisfy
   it by returning the concrete `*AgentConn` (Go has no covariant returns; and its
   `GetAgent` cannot name `api.AgentControl` — same cycle). So **do not change
   `agentapi/server.go`.** Instead add a tiny **composition-root adapter** in
   `cmd/meshserver/main.go` that implements `api.AgentGetter` by delegating to
   `*AgentServer` and converting a missing agent's **typed-nil `*AgentConn` into an
   interface nil** (so handler `ac == nil` checks still hold). This supersedes the
   File-inventory line that said server.go's getters "return the concrete
   `*AgentConn`, which now implements `AgentControl`" — that does not compile.
4. **`Close()` stays off the seam (0.3).** No handler closes an agent conn — it is
   an `agentapi`/registry lifecycle concern ([`deregister.go:53`](../../../server/internal/agentapi/deregister.go#L53)).
   Under the Go-harness mechanism the test **owns** the concrete `AgentServer`/
   `AgentConn` it constructs, so an agent connection-close drill calls
   `Close()`/`DeregisterAgent` on the concrete object directly — the seam needs no
   `Close()`. The seam is exactly the four `Send*` + two `Request*Sync` + `Meta()`
   the api handlers use; the harness substitutes a fault-decorating `AgentControl`
   for the `agent.control-write` scenario. The `main.go` adapter is **production
   wiring** (consumer interface + import cycle), independent of fault injection.
4a. **Fix the metadata data race in this micro-plan (0.4).** `handleRegister`
   writes `OS/Arch/AgentVersion/Capabilities` on the read-loop goroutine while
   `eligibleAgents`/`Meta()` read them on the HTTP goroutine with no lock — a
   pre-existing race. Guard those fields (mutex or atomics); route `Meta()` and
   `requireCapability` reads through the guard. Add a `-race` test that registers
   concurrently with `Meta()`/`eligibleAgents`. This lifts FI0 from "pure
   decoupling" to "decoupling + concurrency fix" — reflect it in the acceptance
   criteria.
5. **Test blast radius (for the implementer).** The api package's test double
   `stubAgentGetter` ([`helpers_test.go:28-50`](../../../server/internal/api/helpers_test.go#L28-L50))
   holds `map[protocol.DeviceID]*agentapi.AgentConn` and returns the concrete
   type — repoint it to `map[protocol.DeviceID]AgentControl` (the two literals in
   `session_handlers_part2_test.go` and `handlers_restart_test.go` change with it).
   Two tests mutate `.Capabilities` on a `GetAgent` result
   (`handlers_restart_part3_test.go`, `handlers_restart_part4_test.go`); once
   `GetAgent` returns the interface, reach the field via a
   `ac.(*agentapi.AgentConn)` type assertion (test-only; the stored value is the
   concrete conn).

## File inventory

**Create**
- (none — the interface lives in the consuming `api` package, Go idiom)

**Modify**
- `server/internal/api/api.go` — add the `AgentControl` interface (its `Meta()`
  returns `agentapi.AgentMeta`; add the `protocol` import for the `Send*`/read
  signatures); change `AgentGetter` to `GetAgent(db.DeviceID) AgentControl` /
  `ListConnectedAgents() []AgentControl` (leave `DeregisterAgent` as-is — it
  already takes `db.DeviceID`, not the conn). **`AgentMeta` is *not* defined
  here** (Decision 2 — it lives in `agentapi`).
- `server/internal/agentapi/conn.go` — add the value struct `AgentMeta` **and**
  `Meta() AgentMeta` so `*AgentConn` satisfies `AgentControl` (Decision 2).
- `server/cmd/meshserver/main.go` — add a small adapter type implementing
  `api.AgentGetter` over `*agentapi.AgentServer` (typed-nil → interface-nil), and
  change `Agents: agentSrv` to pass the adapter (Decision 3). **`agentapi/server.go`
  is left unchanged** — its getters keep returning `*AgentConn`.
- `server/internal/api/handlers_updates.go` — `eligibleAgents` reads via
  `Meta()`; its return type becomes `[]AgentControl`.
- `server/internal/api/handlers_sessions.go`, `handlers_device_actions.go`,
  `handlers_device_inventory.go`, `handlers_device_history.go` — adjust to the
  interface type (call sites already only call `Send*`/`Request*Sync`, so
  minimal). `handlers_devices.go` no longer references the conn.

**Tests (write first — TDD)**
- `server/internal/api/api_test.go` (or a focused `agentcontrol_test.go`) — a
  compile-time assertion `var _ AgentControl = (*agentapi.AgentConn)(nil)`; a
  fake `AgentControl` used by a handler test proving handlers no longer need the
  concrete type; positive (send succeeds) and negative (send returns error →
  handler maps it) cases for at least one `Send*` path, at least one
  `Request*Sync` read path, and the `eligibleAgents` metadata filter.

## Steps (TDD)

1. `git checkout dev && git pull --rebase origin dev`.
2. **Test first:** add the interface-satisfaction assertion + a handler test
   backed by a fake `AgentControl` (this also unblocks the TDD gate for the
   branch). Run — it fails to compile (interface doesn't exist yet).
3. Add `AgentMeta` + `Meta()` to `agentapi/conn.go`; add the `AgentControl`
   interface to `api.go` (returning `agentapi.AgentMeta`) and repoint
   `AgentGetter`. Leave `agentapi/server.go` untouched.
4. Add the `api.AgentGetter` adapter over `*AgentServer` in
   `cmd/meshserver/main.go` (typed-nil → interface-nil); pass it as `Agents:`.
5. Repoint `eligibleAgents` (reads via `Meta()`), the session/updates/device
   handler files, and the api-package test doubles (Decision 5) to the interface.
6. `go build ./...` then `go test ./internal/api/... ./internal/agentapi/...` green.
7. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Acceptance criteria

1. No `api` package file references `*agentapi.AgentConn` directly (grep clean).
2. `*agentapi.AgentConn` satisfies `AgentControl` (compile-time assertion).
3. Handlers tested against a fake `AgentControl` (no real connection needed).
4. `eligibleAgents` filtering behavior unchanged (test asserts os/arch/version).
5. The metadata race is fixed: `OS/Arch/AgentVersion/Capabilities` are guarded,
   read via `Meta()`/`requireCapability` under the guard, with a `-race` test that
   registers concurrently with `Meta()`/`eligibleAgents` (0.4).
6. No fault-injection code introduced.
7. Full gauntlet green (incl. `go test -race`).

## Reviewer checklist

- [ ] Interface defined in the **consumer** (`api`) package, not `agentapi`.
- [ ] `AgentControl` surface is exactly the api package's four `Send*`
      control-writes + two `Request*Sync` reads (+ `Meta()`);
      `SendRequestDeviceLogs` (now behind `RequestLogsSync`) and
      `SendAgentDeregistered` correctly excluded; capability errors preserved.
- [ ] Metadata-access decision implemented as chosen; `AgentMeta` defined in
      `agentapi` (Decision 2); no field/method name collision; no unrelated
      readers of `OS/Arch/AgentVersion/DeviceID` broken.
- [ ] Composition-root adapter (`main.go`) returns an **interface nil** for a
      missing agent — not a typed-nil `*AgentConn` wrapped in `AgentControl`
      (which is non-nil) — so `GetAgent(...) == nil` handler checks still fire.
- [ ] `agentapi/server.go` getters unchanged; `*AgentServer` is bridged via the
      adapter, not by mutating its return types (Decision 3).
- [ ] `go-arch-lint check` still green (no new cross-boundary dependency).
- [ ] Tests cover positive + negative `Send*` and the `eligibleAgents` filter.
- [ ] No behavior change — pure decoupling; `make golden`/e2e unaffected.
