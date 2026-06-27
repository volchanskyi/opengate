# Micro-Plan FI1 — Fault-injection specification & safety contract (+ ADR)

**Master:** `context-driven-fault-injection.md` §11 (FI1), §5, §10, §14.
**Branch:** `dev`. **Owner:** engineer (Go + docs). **Sequence:** after FI0, before FI2. **Depends on:** FI0 (the `AgentControl` seam exists, so `agent.control-write` can be listed as a real fault point).
**Status:** Ready after FI0. **This micro-plan is spec + ADR + a benchmark — minimal production code.**

## Goal

Freeze the contract FI2+ build against: the enumerated fault points, the typed
action set, the profile schema, the safety invariants (staging-only,
production-deny, fail-closed), the per-scenario expected outcomes, and the
**mechanism choice** (compiled-in injector vs adapter-substitution) made with a
disabled-overhead benchmark in hand. Record it as an ADR (this introduces a
privileged, cross-cutting testing mechanism into the live server).

## Deliverables

1. **Spec doc** — `docs/Fault-Injection.md` (new), the SSOT for FI2–FI6:
   - **Fault points** (post-teardown, master §6): `api.before-handler`,
     `session.repository`, `device.repository`, `relay.registry`,
     `relay.session-drop` *(deferred)*, `notifications.dispatch`, `amt.operator`,
     `agent.control-write` (now wrappable post-FI0), `websocket.before-upgrade`.
     Mark the **gating core** = `session.repository`, `device.repository`,
     `api.before-handler` (all already interfaces in `ServerConfig`,
     [`api.go:51-76`](../../server/internal/api/api.go#L51-L76)).
   - **Action set** (master §5): `delay`, `timeout`, `error`, `panic`,
     `blocked` (wait on ctx cancel — replaces literal deadlock), `connection-close`.
   - **Profile schema** — named profile → allowed fault points → action params,
     with **max duration** and **concurrency caps**; schema-validated, not free
     shell. Profiles: `smoke` (gating), `infra` (scheduled), `network` (deferred).
   - **Per-scenario expected outcomes** — exact HTTP status + typed error class
     per row of master §7.
   - **Safety invariants:** enabled only when `environment=staging`; the
     fault-selecting listener bound to the **cluster-internal path only**
     (Model C, master Decision 5); fail-closed conditions (master §5).
2. **ADR** — `docs/adr/ADR-041-fault-injection-mechanism.md` + index row in
   [`.claude/decisions.md`](../decisions.md): records the privileged-testing
   decision, Model C activation, staging-only/production-deny, and the §3
   mechanism choice with the benchmark number. (FI links only the **archived**
   master plan, never an active plan — per the adr-plan-link guard.)
3. **Disabled-overhead benchmark** — a Go benchmark proving the disabled path is
   a single branch with no goroutine/timer allocation (informs the mechanism
   choice and seeds FI2's NFR test).

## File inventory

**Create**
- `docs/Fault-Injection.md`
- `docs/adr/ADR-041-fault-injection-mechanism.md`
- `server/internal/faultinject/spec_test.go` — the disabled-overhead benchmark +
  a table of the enumerated profiles/points/actions used as a schema fixture
  (write the benchmark first; it exercises a trivial disabled stub).

**Modify**
- `.claude/decisions.md` — ADR-041 index row.
- `docs/Home.md` — link the new Fault-Injection doc.

## Steps (TDD)

1. After FI0 merges, branch is current.
2. **Benchmark first:** add `BenchmarkInjectorDisabled` against a minimal
   disabled-state stub; capture allocs/op and ns/op. This is the empirical input
   to the mechanism decision (master Decision 7).
3. Write `docs/Fault-Injection.md` (spec) and ADR-041 (decision + benchmark
   number + Model C + production-deny).
4. Confirm the spec's fault-point list matches the actual `ServerConfig` ports
   and the post-FI0 `AgentControl` seam (no point references deleted code:
   `relay.peer-dial`, Redis — master §3 "Out of scope").
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Settled inputs (do not re-litigate — master §14)

- Pod-recreation SLO = **120 s**. Activation = **Model C** (cluster-internal).
- Gating core = `session.repository`, `device.repository`, `api.before-handler`.
- Smoke profile gates promotion with **app faults only**; infra drills are
  scheduled evidence.

## Open for FI1 to settle (master §14)

1. Recovery SLOs for relay/agent reconnect and rollout rollback — **measure
   first** (several scheduled drills; set the threshold above observed p95).
2. Whether ingress 502 uses a reviewed snippet or only an upstream failure
   (prefer the latter; defer until the security contract is tightened — FI4).
3. **Mechanism choice** — compiled-in injector vs adapter-substitution, decided
   with the benchmark in hand. Record the choice + rationale in ADR-041.

## Acceptance criteria

1. `docs/Fault-Injection.md` enumerates every fault point, action, and profile
   with max-duration/concurrency caps and per-scenario expected outcomes.
2. ADR-041 records the mechanism decision (with the benchmark number), Model C,
   and the production-deny invariant; index row added.
3. Benchmark shows the disabled path allocates no goroutine/timer.
4. No spec entry references deleted (pre-teardown) fault points.
5. Gauntlet green; doc-links pass.

## Reviewer checklist

- [ ] Every fault point maps to a real `ServerConfig` port or the FI0 seam.
- [ ] Profile schema has explicit max-duration + concurrency caps.
- [ ] Production-deny + staging-only invariants stated and testable.
- [ ] Mechanism choice is justified by the benchmark, not asserted.
- [ ] ADR-041 links only archived plans / ADRs / code / external URLs.
- [ ] `docs/Fault-Injection.md` linked from Home; no paraphrased numbers (link to
      source per `docs/README.md`).
