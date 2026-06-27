# Service-Extraction Decision Lens — Balanced Coupling

**Type:** Decision lens (scoring rubric), **not** an active implementation plan.
**Status:** Active as a lens. **Current decision: no service extraction.**
**Last refreshed:** 2026-06-26 (data window: churn = commits last 120 days).

Adopts Vlad Khononov's **Balanced Coupling** model as the explicit lens for "what,
if anything, do we pull out of the modular monolith next, and when." Distilled
from the [`vladikk/modularity`](https://github.com/vladikk/modularity) framework.
This is a lens recorded per-extraction in ADRs, **not** a CI gate —
[`go-arch-lint`](../../server/.go-arch-lint.yml) stays the deterministic boundary
enforcer (for the constrained leaves; see the scoping note below), PMAT churn
feeds the volatility axis, and the LLM `modularity` plugin is advisory only.

## What this is / isn't

- **Is:** a repeatable way to score a module before proposing a boundary change,
  so the decision is data-driven and recorded in an ADR.
- **Isn't:** a backlog of extractions to execute. As of the last refresh, the
  production target is **OCI free-tier / single-node, one server replica**, and
  **no concrete scale, availability, or isolation driver exists** — so the lens
  output is "extract nothing; decouple in-place where fan-in is high."

Relay scale-out is **not** tracked here. The multi-server relay design was
evaluated and **removed** (local-pairing-only today; slim in-process
`SessionRegistry` seam retained). The single sources of truth for any future
relay scale-out are [ADR-023](../../docs/adr/ADR-023-relay-extraction-redis-session-registry.md)
and [`Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md). See
"Relay: status correction" below.

## The rubric (the three axes)

Score each candidate module on:

1. **Integration strength** — how it couples to the rest: intrusive → functional →
   model → **contract** (lowest/best). Lower strength ⇒ cheaper to put behind a
   network boundary. **Scoping caveat (do not overstate):** `go-arch-lint` proves
   contract-level integration only for the **constrained leaves** it lists —
   `cert, audit, update, auth, device, notifications, session, usecase` (each
   `mayDependOn: []`). The usual extraction *candidates* — `api`, `agentapi`,
   `relay`, `protocol`, `db`, `amt/transport` — sit in the unconstrained `other`
   catch-all (`anyProjectDeps: true`). So "ports exist" must be **proven
   per-module** before extraction; a green `go-arch-lint check` is necessary, not
   sufficient, and is *not* repo-wide service-readiness proof.
2. **Distance cost = fan-in** — number of distinct internal **production**
   packages that would now cross a process/network boundary if the module were
   extracted (test-support packages excluded). Lower ⇒ cheaper.
3. **Volatility = churn** — how often it changes. High volatility **+** high
   distance = maximum maintenance pain; but volatility also *justifies* extraction
   when it pairs with a real independent-scaling / availability / isolation driver.

**Decision rule:** extract a module only when **(a)** its integration strength is
contract-level **and (b)** its fan-in is low **and (c)** there is a concrete
scaling/availability/security-isolation driver. If fan-in is high → **decouple
in-place, do not extract**. If volatility is low → **leave it alone**. If no
driver exists (the current case) → **extract nothing**.

## Current scores

Churn = `git log --since=<120d> -- server/internal/<mod>/`; fan-in = distinct
internal production importers (test-support excluded). Refresh both before using
the lens for any decision.

| Module | Volatility (churn) | Fan-in | Strength | Verdict |
|---|---|---|---|---|
| api | 89 | 1 | contract (`other`, unconstrained) | **stay in-process** — composition root; churn doesn't cascade (fan-in 1); distance would be pure latency |
| db | 55 | **4** | shared infra (`other`) | **decouple-in-place, never service-split** — central persistence/migration/pool; continue ADR-021 per-aggregate repos |
| agentapi | 41 | 2 | contract (`other`) | stay in-process — active reconnect/handshake work; a boundary adds latency + ops cost |
| relay | 27 | 3 | contract (seam only) | **stay** — see status correction; live path is local pairing only |
| protocol | 23 | 5–6 | *is* the wire contract | stays a shared lib (high fan-in is correct; golden/fuzz/property-tested) |
| notifications | 21 | 5–6 | contract (constrained leaf) | stay (watch fan-out — see triggers) |
| amt/transport | 12 | 3 | contract (`other`) | stay (watch — security isolation) |
| cert/updater/auth/device/session/audit/usecase | low | low | contract (constrained leaves) | **leave alone** — low volatility |

Note the **relay churn is mostly self-inflicted**: relay's entire commit history
falls inside the 120-day window and a large share of its 27 commits is the
*add-then-teardown* of the now-removed multi-server infrastructure, not organic
feature pressure. Its volatility therefore overstates any extraction case.

## Current verdict (no driver → no extraction)

- **`relay` — stay.** Local-pairing-only; the multi-server design is removed. Any
  rebuild is governed by Multiscale-Readiness, not by this lens.
- **`db` — decouple in-place (ADR-021), never service-split.** Highest-coupling
  central infra (fan-in 4: `cmd/meshserver`, `api`, `amt`, `amt/transport`) + high
  volatility (55): a DB *service* boundary would be maximal pain. The right move is
  the in-monolith per-aggregate repository extraction already underway.
- **`api`/`agentapi` — stay in-process.** High volatility, low fan-in → distance
  buys nothing and costs latency.
- **Stable leaves — leave alone.** Low volatility ⇒ no decoupling investment.

## Relay: status correction

The earlier version of this file listed `relay → multi-server` as "in progress
(finish C3, then PR-D/E)." That is **stale and must not be acted on**: the Redis
adapter, Sentinel topology, ownership operations, cross-server WebSocket proxy,
internal listener, degraded-mode state machine, and multiserver harness were
**removed** ([ADR-023](../../docs/adr/ADR-023-relay-extraction-redis-session-registry.md);
phases.md "Dormant Multi-Replica Teardown"). The `PeerDialer` and internal-listener
ports no longer exist; only the slim `SessionRegistry`
([`registry.go`](../../server/internal/relay/registry.go)) remains. Following the
old roadmap would reintroduce removed dormant distributed infrastructure without
the operational evidence its removal required. Multi-replica routing is a
**rebuild with explicit readiness gates** ([`Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) §8),
not a configuration switch.

## Future triggers (when to re-score)

Revisit a module for extraction only when it gains a concrete driver **and** keeps
low fan-in / contract strength **and** the per-module contract-readiness is proven
(not assumed from the repo-wide `go-arch-lint` pass):

- **`amt/transport`** — security isolation (handles Intel AMT/MPS TLS); could earn
  a boundary if blast-radius isolation becomes a requirement. Not now (churn 12, no
  driver).
- **`notifications`** — if web-push fan-out becomes a throughput hotspot, an async
  queue/worker split could be justified. Not now (no scaling pressure).
- **Any relay scale-out** — gated entirely by [`Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md)
  and a funded Large-tier decision, not by this lens.
- Any module whose **volatility rises while fan-in stays low** and a
  scale/availability/security driver emerges.

## Quality gates for any future extraction

Before implementing any extraction, the proposal must define acceptance evidence
for: **performance** (p50/p95/p99 on the affected and cross-replica paths);
**availability** (rolling-update, drain, owner-loss, registry-loss, DB-failover,
rollback drills); **security** (peer authentication, NetworkPolicy isolation,
token redaction, no public internal control plane); **maintainability** (a
*dedicated* `go-arch-lint` boundary for the candidate — it must graduate out of
`other` — tests-first, coverage above the project floor, PMAT/Clean-as-You-Code
clean); **observability** (ownership, registry, peer-route, reconnect-storm,
saturation, alert metrics); **operability** (backup/restore, failover,
scale-up/down, rollback runbooks); and **cost** (explicit free-tier vs paid-tier
decision; no hidden always-on infrastructure).

## Why this confirms current decisions

Applying the lens to current data validates the existing ADR-driven path: no
extraction is justified, `api`/`agentapi` stay monolithic, `db` is the hotspot
ADR-021 already targets in-place, and the stable leaves stay simple. The durable
addition is making **volatility an explicit, scored axis** in extraction ADRs
rather than implicit prose, and **scoping the "contract-level" claim** to the
modules `go-arch-lint` actually constrains.

The current engineering investment correctly goes to single-replica reliability,
not distribution: the in-flight `context-driven-fault-injection` master plan
(FI0–FI6) and the `td-agent-session-resumption-cache` micro-plan (agent
key-permission hardening + in-process TLS-resumption observability, tracked under
the "W3 decision" entry in [`techdebt.md`](../techdebt.md)).

## Follow-ups

- **Optional:** formalize the lens itself as an ADR (next free number on disk is
  ADR-040) if we want it immutable rather than a plan-located note. Record the
  pointer in [`.claude/decisions.md`](../decisions.md), which can be kept current.
- **Optional:** pilot `/modularity:review` on `db` once to compare its LLM output
  against this churn/fan-in analysis — advisory only, never a gate.
- Refresh the score table from current churn/fan-in each time an extraction is
  considered (the table above is a snapshot, not a standing fact).
