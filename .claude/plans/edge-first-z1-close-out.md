# EF-Z1 — Program close-out: evidence, E2E, docs, ADRs, tech debt, archive

**Master plan:** `edge-first-telemetry-and-investigations.md` §14.2, §14.3, D25, step 22.
**Acceptance criteria owned:** none directly — this plan proves the **whole set** is met and records
what the program deliberately did not do.
**Dependencies:** every other EF micro-plan.
**Blocks:** nothing.

## Deliverables

### 1. Measurement evidence (the D36 obligation)

Three numbers were promised as **measured**, and each carried a decision. Collect them in one place
with method, date and tool version — an archived plan is deletion-bound, so the record must live in
`/docs`, not only in a plan:

| # | From | What must be recorded |
|---|---|---|
| Q3 | EF-B3 | Recorded in [Monitoring.md](../../docs/Monitoring.md) (“What an active series costs the central store”): the fit, the 120 000-series total, method, VM version and date. Still to add there: **which of §9.1's four sizing options the owner chose** |
| Q11 | EF-B10 | **Done** — recorded in [Monitoring.md](../../docs/Monitoring.md) ("Has this happened before?") and [ADR-072](../../docs/adr/ADR-072-retroactive-rule-evaluation.md): ~7 months at the shipped cap, from 2/4/8 MiB steady-state runs whose reach scales with the cap to within 5 %, at 13 series × 1 Hz and ~2.3 B per stored reading. The measurement itself always runs, in [reach_test.rs](../../agent/crates/mesh-agent-core/tests/reach_test.rs) |
| Q12 | EF-C4 | Alerts/device/day, the population it was measured on, and the §9.1.2 option taken — or an explicit statement that no real-population measurement exists yet and the pack is held at canary |

Q2 and Q4 (active series and 30 d disk at fleet scale) are recorded in the same section; verify they still match what the harness reports.

### 2. End-to-end

`make e2e` (never bare `npx playwright test`) covering the full path: an agent emits a vital → a rule
fires → an alert with evidence lands → an incident opens → a tech resolves it with a cause code.

### 3. Docs

Reconcile every page the program touched: [Monitoring.md](../../docs/Monitoring.md),
[Architecture.md](../../docs/Architecture.md), [Wire-Protocol.md](../../docs/Wire-Protocol.md),
[Database.md](../../docs/Database.md), [API-Reference.md](../../docs/API-Reference.md),
[Data-Lifecycle.md](../../docs/Data-Lifecycle.md), [Testing.md](../../docs/Testing.md),
[Multiscale-Readiness.md](../../docs/Multiscale-Readiness.md).

Per [docs-live-state.md](../rules/docs-live-state.md): describe **only what is now live**. The 10 s
central stream, `/correlate` and drag-to-correlate are gone — say what the system does, do not
narrate their removal. Per [plans-and-adrs.md](../rules/plans-and-adrs.md), **no doc under `docs/`
outside an ADR may link a plan at all**.

### 4. ADRs (next free number is **064** — 063 is the current maximum)

Three decisions are genuinely separable; splitting them keeps each ADR readable and independently
supersedable:

| ADR | Subject |
|---|---|
| 064 | **Edge-first vitals contract** — O(1) cardinality, the 24 cap, 60 s cadence, extrema, the `disk.used_percent` **meaning change** to worst-mount, Linux-only stall and disk-performance vitals, `%util` excluded |
| 065 | **Self-contained alerts and the incident model** — evidence as an immutable DEFLATE blob on the alert row, two-axis grouping, `reopen_window` = `group_window`, closed severity/cause-code sets, ceilings |
| 066 | **Curated rule catalogue and rollout** — embedded definitions + Postgres bindings/rollout, selector resolution, CI cost gate, canary/staged/full with automatic revert, kill as a row flip |

Each gets an index row in [decisions.md](../decisions.md). ADRs may link **archived** plans only.
The `disk.used_percent` meaning change must be called out explicitly in 064 — an existing
`disk.used` rule keeps firing but now means something different, and that surprise belongs in the
decision record, not in a changelog nobody reads.

### 5. Tech-debt rows (D25 — a close-out **deliverable**, not a note in a plan)

Add to [techdebt.md](../techdebt.md):

- **§14.2 — alert state in VictoriaMetrics deferred.** The three existing WS-19 per-device series are
  unchanged. Revisit when **both** are known: the real fleet-board query shape (once a tech has used
  the incident API) and a measured alert volume at Contoso scale.
- **§14.3 — 1 y retention declared, no sweep.** Erasure still cascades (I9) and §6.7's counters make
  growth observable. Revisit when measured growth projects past ~10 GB/year, or a compliance
  commitment makes 1 y contractual.

### 6. `phases.md` and archiving

One **Completed** row in [phases.md](../phases.md) linking
`plans/archive/edge-first-telemetry-and-investigations.md`, and the master plan `git mv`-ed into
`archive/` **in the same commit** — the consistency gate refuses a Completed row whose plan link
resolves to a non-archived plan. Bump the master plan's internal links one `../` deeper and validate
with `GO111MODULE=off go run ./scripts/check-doc-links`.

Every EF micro-plan should already be archived by its own landing commit; verify none was forgotten.

### 7. Gap-closure record

Four gaps were found while decomposing the master plan and resolved inside micro-plans. Record where
each landed, so the master plan's own §12 is not left describing a smaller program than shipped:

1. **Coverage had no wire transport** (§7.5) → additive `rule_coverage[]` on `AgentHealthSummary`
   (EF-B8).
2. **`RuleCoverage` had no storage** (§7.4) → in-memory, `unknown` derived (EF-B8).
3. **Evidence cap 64 KB collides with `maxTelemetryPayloadBytes` 64 KiB** → a separate alert payload
   bound (EF-C1).
4. **Step 21 (web) had no acceptance criterion** (§12) → W1–W6 (EF-C6).

## File inventory

- **Modify:** the eight `docs/` pages listed in deliverable 3.
- **Create:** `docs/adr/ADR-064-*.md`, `ADR-065-*.md`, `ADR-066-*.md` (confirm the next free number
  at implementation time).
- **Modify:** [decisions.md](../decisions.md) — three index rows.
- **Modify:** [techdebt.md](../techdebt.md) — the two rows in deliverable 5.
- **Modify:** [phases.md](../phases.md) — one Completed row.
- **Create:** a spec in [web/e2e/](../../web/e2e/) — the end-to-end triage flow.
- **Move:** `edge-first-telemetry-and-investigations.md` → `archive/`, links bumped one `../` deeper.

## Steps

1. Collect the three measurements and the owner decisions into the docs.
2. Run and land the E2E flow.
3. Reconcile docs; run `GO111MODULE=off go run ./scripts/check-doc-links`.
4. Write ADR-064/065/066 + `decisions.md` rows.
5. Add both tech-debt rows.
6. `phases.md` Completed row + archive the master plan, one commit.
7. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Reviewer checklist

- [ ] Q3, Q11, Q12 recorded with method and the owner's decision — no estimate presented as measured.
- [ ] `make e2e` covers vital → rule → alert+evidence → incident → resolution.
- [ ] No doc narrates removed features; no `docs/` page outside an ADR links a plan.
- [ ] ADR-064/065/066 written, indexed, and linking only archived plans.
- [ ] Both tech-debt rows present with their named revisit triggers.
- [ ] `phases.md` Completed row + master plan archived in the same commit; doc-links gate green.
- [ ] Every EF micro-plan is in `archive/`.

## Verification

`make e2e`, `GO111MODULE=off go run ./scripts/check-doc-links`,
`bash scripts/tests/plans-archive-consistency.test.sh`, `/precommit`.
