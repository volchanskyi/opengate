# Service-Extraction Roadmap — Balanced-Coupling lens

**Created:** 2026-06-05 · **Status:** Active (decision lens; revisit when churn/scale drivers change)

Adopts Vlad Khononov's **Balanced Coupling** model as the explicit lens for "what
do we pull out of the modular monolith next, and when." Distilled from the
[`vladikk/modularity`](https://github.com/vladikk/modularity) framework discussion.
This is a **decision lens recorded per-extraction in ADRs**, not a CI gate —
[`go-arch-lint`](../../server/.go-arch-lint.yml) stays the deterministic boundary
enforcer; PMAT churn feeds the volatility axis; the LLM `modularity` plugin is
optional advisory only.

## The rubric (the three axes)

Score each candidate module on:

1. **Integration strength** — how it couples to the rest: intrusive → functional →
   model → **contract** (lowest/best). OpenGate is **contract-level repo-wide**,
   CI-proven: every leaf domain has `mayDependOn: []` in `.go-arch-lint.yml`, so
   modules can only integrate through ports. Lower strength ⇒ cheaper to put
   behind a network boundary.
2. **Distance cost = fan-in** — number of internal packages that would now cross a
   process/network boundary if the module were extracted. Lower ⇒ cheaper.
3. **Volatility = churn** — how often it changes. High volatility **+** high
   distance = maximum maintenance pain; but volatility also *justifies* extraction
   when it pairs with a real independent-scaling / availability / isolation driver.

**Decision rule:** extract a module only when **(a)** its integration strength is
contract-level **and (b)** its fan-in is low **and (c)** there is a concrete
scaling/availability/security-isolation driver. If fan-in is high → **decouple
in-place, do not extract**. If volatility is low → **leave it alone** (don't
over-engineer a stable module).

## Current scores (server, churn = commits last 120d; fan-in = distinct internal importers)

| Module | Volatility | Fan-in | Strength | Verdict |
|---|---|---|---|---|
| api | 80 | 1 | contract | **stay in-process** — composition root; churn doesn't cascade (fan-in 1); distance would be pure pain |
| db | 54 | **8** | contract (splitting) | **decouple-in-place, never extract** — highest fan-in; continue ADR-021 per-aggregate repos |
| agentapi | 36 | 2 | contract | stay in-process |
| **relay** | 21 | 3 | **contract** (ports) | **EXTRACT — in progress** (ADR-023/031/033) |
| protocol | 20 | 6 | *is* the wire contract | stays a shared lib (high fan-in is correct; golden-tested) |
| notifications | 20 | 6 | contract | stay (watch — see triggers) |
| amt/transport | 10 | 3 | contract | stay (watch — security isolation) |
| cert/updater/auth | 6–13 | 4 | contract | stay |
| session/device/audit/usecase | 1–2 | 1–5 | contract | **leave alone** — low volatility |

## Roadmap

### Now (in progress): `relay` → multi-server
Contract strength (the `SessionRegistry`/`PeerDialer`/internal-listener ports built
in PR-C C1/C2), low fan-in (3), moderate volatility (21), real driver (horizontal
scale + cross-server session affinity). Textbook balanced extraction. Finish
**C3** (degraded-mode + `registry_up` metric + Telegram alert), then PR-D/E.

### Next: **no second extraction is justified by the data.** Instead:
- **`db` — decouple in-place (ADR-021), do not service-split.** Highest fan-in (8) +
  high volatility (54): a DB *service* boundary would be maximal pain. The right
  move is the in-monolith per-aggregate repository extraction already underway. A
  DB split stays off the table until fan-in drops materially **and** a scaling
  driver appears.
- **`api`/`agentapi` — stay in-process.** High volatility, low fan-in → distance
  buys nothing and costs latency.
- **Stable leaves — leave alone.** Low volatility ⇒ no decoupling investment.

### Future triggers (when to re-score)
Revisit a module for extraction only when it gains a concrete driver **and** keeps
low fan-in / contract strength:
- **`amt/transport`** — security isolation (handles Intel AMT/MPS TLS); could earn a
  boundary if blast-radius isolation becomes a requirement. Not now (churn 10).
- **`notifications`** — if web-push fan-out becomes a throughput hotspot, an async
  queue/worker split could be justified. Not now (no scaling pressure).
- Any module whose **volatility rises while fan-in stays low** and a scale/availability
  driver emerges.

## Why this mostly *confirms* current decisions
Applying the lens to real data validates the existing ADR-driven path: relay is the
right (only) extraction; api stays monolithic; db is the hotspot ADR-021 already
targets; stable leaves stay simple. The new, durable addition is making
**volatility an explicit, scored axis** in extraction ADRs rather than implicit
prose (cf. ADR-023's "memberlist deferred until >20 servers").

## Open follow-ups
- Optional: formalize the lens itself as an ADR (e.g. "ADR-034: Balanced-Coupling
  as the extraction-decision lens") if we want it immutable rather than a plan.
- Optional: pilot `/modularity:review` on `db` or `relay` once, to compare its
  LLM output against this churn/fan-in analysis — advisory only, never a gate.
- Refresh the score table from PMAT churn each time an extraction is considered.
