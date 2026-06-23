# Master Plan: Diagrams as Code — Part 2

**Status:** Broken into D1–D5 micro-plans (§11); awaiting per-plan approval.
Do **not** implement from this file directly — implement from the D-series micro-plans.
**Branch:** `dev`.
**Part 1:** [`docs-doctrine-e-diagrams-as-code.md`](archive/docs-doctrine-e-diagrams-as-code.md)
(archived; the original Mermaid-only docs-as-code decision).

---

## 1. Why a Part 2

Part 1 (DD-E) established **Mermaid-only docs-as-code** and shipped a first set of
hand-curated diagrams. Reviewing it surfaced four gaps: **drift risk / weak supervision**,
**no C4 methodology**, **no Mermaid syntax validation**, and **ad-hoc coverage**. This
plan resolves each with a locked decision and an engineer-ready breakdown.

---

## 2. Verified current state (re-verified 2026-06-18 — corrects the earlier draft)

Direct measurement (`grep -c '^```mermaid$'`) — the earlier draft said "6 across 4 docs";
the **actual** state is **8 Mermaid fences across 6 docs**:

| Doc | Mermaid blocks |
|---|---|
| [`docs/Architecture.md`](../../docs/Architecture.md) | 3 (`flowchart` topology + 2 `sequenceDiagram`) |
| [`docs/Wire-Protocol.md`](../../docs/Wire-Protocol.md) | 1 `sequenceDiagram` |
| [`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) | 1 `flowchart` |
| [`docs/Monitoring.md`](../../docs/Monitoring.md) | 1 `flowchart` *(missed by the draft)* |
| [`docs/adr/ADR-025-cd-preflight-digest-check.md`](../../docs/adr/ADR-025-cd-preflight-digest-check.md) | 1 *(missed by the draft)* |
| [`docs/README.md`](../../docs/README.md) | 1 (convention example) |

- Types in use: **5 `flowchart` + 3 `sequenceDiagram`**. **C4 is absent** repo-wide
  (verified: no `C4Context`/`C4Container`/`C4Component`/`C4Dynamic` anywhere).
- **Enforcement gap (key finding):** [`docs-diagrams.test.sh`](../../scripts/tests/docs-diagrams.test.sh)
  pins only **2** of the 6 docs (Architecture ≥3, Multiscale ≥1) + the blob/fence bans +
  the boundary-linter wiring. The Wire-Protocol, Monitoring, ADR-025, and README diagrams
  are **unguarded** — they can vanish silently. (That the draft itself drifted is direct
  evidence of the §3.1 risk.)
- **Hook posture (corrected):** the gauntlet's "shell tests" step runs
  `scripts/tests/*.test.sh` (grep-only, **zero-network**, sub-second) — see
  [`precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh). The earlier draft's
  "<1.5s" figure is not documented anywhere; the real, verifiable constraint is
  *deterministic + zero-network + no heavy local runtime*.

---

## 3. Decisions locked (this round)

| # | Area | Decision |
|---|---|---|
| 1 | Drift/supervision | **Tighten the test + CODEOWNERS nudge** — pin all diagram-bearing docs; nudge a diagram review when handshake/relay/topology code changes. |
| 2 | Methodology | **Adopt native Mermaid C4 blocks** (`C4Context`/`C4Container`/`C4Component`/`C4Dynamic`). |
| 3 | Syntax validation | **CI-only Node validator** (no Puppeteer); local hook stays grep-only. |
| 4 | Coverage | **Minimal "what-must-be-diagrammed" standard + add high-value diagrams.** |

### 3.1 C4 risk control (because native C4 was chosen)

Research (sources §10) shows native Mermaid C4 is **experimental** and has **known
GitHub-rendering issues** (renders in the Live editor, flaky on GitHub). The decision
stands, but D2 **must** carry:

- a **mandatory GitHub render-verification gate**: every C4 block is confirmed to render
  on an actual GitHub PR preview (not an error box) before merge — captured as evidence;
- a **documented per-diagram fallback**: if a C4 block won't render, fall back to the
  proven `flowchart`/`sequenceDiagram` notation arranged along C4 *levels* (same
  information, robust rendering);
- **version alignment** between the D3 CI validator's Mermaid and GitHub's renderer (a
  validator on a different Mermaid version can disagree with GitHub — the render gate is
  authoritative, the validator is a fast pre-filter).

---

## 4. Scope

**In:** harden drift enforcement; adopt C4 (Context + Container at minimum, Component for
server internals optional); a CI-only syntax validator; a coverage standard + the
top-priority new diagrams; an ADR + doc updates.
**Out:** any local-hook heavy runtime; committed image blobs; D2/SVG pipeline;
source-AST graph extraction (rejected in Part 1 — structural drift stays the boundary
linters' job); web-perf diagrams.

---

## 5. Workstreams (basis for the D1–D5 micro-plan breakdown)

- **D1 — Drift-guard hardening (do first; closes the proven gap).**
  Extend [`docs-diagrams.test.sh`](../../scripts/tests/docs-diagrams.test.sh) to pin a
  minimum Mermaid count for **every** diagram-bearing doc (Architecture, Wire-Protocol,
  Multiscale, Monitoring, ADR-025) + a total-fence floor. Add a `.github/CODEOWNERS`
  entry and/or PR-template checklist item nudging a diagram review when
  `agent/**`/`server/internal/agentapi/**`/relay/topology code changes. TDD (test edited
  before it guards new docs).
- **D2 — C4 adoption (native blocks + render gate).**
  Add a **C4Context (L1)** view; express the topology as **C4Container**; optional
  **C4Component** for server internals; consider **C4Dynamic** for a key flow. Update the
  [`docs/README.md`](../../docs/README.md) convention to standardize the allowed C4 block
  types. **Gate:** GitHub render-verification evidence per C4 block; fallback per §3.1.
- **D3 — CI-only Mermaid syntax validation.**
  A CI job (Node — already present in CI) running a **no-Puppeteer** validator over every
  `mermaid` fence in `docs/`; fail CI on a syntax error. Tool must be **pinned**, **handle
  our block types incl. C4**, and be **version-aligned to GitHub's Mermaid** as closely as
  possible. Candidates: `@zabaca/mermaid-validate` (official parser + jsdom — most likely
  to accept experimental C4) or `probelabs/maid` (pure-JS — confirm C4 grammar coverage
  before choosing). **Not** added to the local gauntlet.
- **D4 — Coverage standard + high-value additions.**
  Define a minimal "what must be diagrammed" standard in `docs/README.md`; add the
  top-gap diagrams (candidates: deploy/OKE topology, session lifecycle, CI/CD flow); pin
  each new diagram in D1's test.
- **D5 — ADR + docs/decisions/phases.**
  New mutable ADR recording the Part-2 decisions (C4 adoption + the render-gate caveat,
  CI syntax lint, drift-guard, coverage standard); `decisions.md` row; `phases.md` rows.

---

## 6. Quality metrics & NFRs

**Quality / DoD signals:**
- D1: every diagram-bearing doc is pinned; deleting any guarded diagram **reds** the
  shell-tests step (prove by a local removal).
- D2: each C4 block has attached **GitHub render evidence**; README documents the C4
  convention.
- D3: a deliberately malformed Mermaid block **fails CI**; a valid block (incl. C4)
  passes; the validator is pinned.
- D4: the standard is documented and the added diagrams are pinned by D1's test.
- New Bash passes `make shell-quality`; `/precommit` green per commit; TDD throughout.

**NFRs:**
- **Performance:** local hook stays grep-fast/zero-network; **all** Mermaid parsing runs
  in CI only (Node), never in the local gauntlet.
- **Security:** a validator enters the trusted CI build path — pin it by version/digest,
  prefer a maintained tool, and scope it to `docs/**` (no repo-wide code exposure).
- **Maintainability:** drift checks are **structural/advisory, not false-gating** (no
  source-AST extraction); C4 reuses one engine (Mermaid) with a documented fallback so a
  renderer regression never blocks docs.

## 7. Constraints (inherited from DD-E)

Mermaid-only; no committed image blobs; no D2/SVG pipeline; no heavy local-hook runtime;
structural-boundary drift remains the boundary linters' job (`go-arch-lint`,
`cargo modules`, `dependency-cruiser`). Any new check must be deterministic +
zero-network **locally** (CI may use Node).

## 8. Sequencing

D1 (immediate gap closure) → D3 (validation infra, so later diagram edits are checked) →
D2 (C4, validated by D3 + the render gate) → D4 (new diagrams, guarded by D1+D3) → D5
(documents the landed state). Each micro-plan keeps the gauntlet green per commit.

## 9. Master acceptance criteria

- [ ] Every diagram-bearing doc is pinned by `docs-diagrams.test.sh`; a removed diagram
      reds the run.
- [ ] CODEOWNERS/PR nudge fires on handshake/relay/topology code changes.
- [ ] C4 Context + Container views exist and **render on GitHub** (evidence attached);
      README documents the C4 convention; fallback documented.
- [ ] A CI syntax-validation job fails on malformed Mermaid (incl. C4) and is pinned.
- [ ] Coverage standard documented; high-value diagrams added + pinned.
- [ ] ADR + `decisions.md` + `phases.md` updated; `/precommit` green.

## 10. Research sources (C4-on-GitHub + validator feasibility)

- Mermaid C4 syntax (official, marked experimental): https://mermaid.js.org/syntax/c4.html
- C4 rendering issues on GitHub (vs Live editor): https://github.com/mermaid-js/mermaid/issues/3217
- No-Puppeteer validators: https://github.com/probelabs/maid ,
  https://github.com/Zabaca/mermaid-validate ; markdown-aware: https://github.com/suwa-sh/md-mermaid-lint

## 11. Micro-plan breakdown (engineer-ready)

Each is self-contained (file inventory, steps, acceptance/DoD, reviewer checklist).
Referenced by filename (active plans cannot be linked; all live in `.claude/plans/`).

| WS | Micro-plan file | Depends on | Notes |
|---|---|---|---|
| D1 | `diagrams-d1-drift-guard.md` | — | do first; closes the proven 2-of-6 test gap |
| D2 | `diagrams-d2-c4-adoption.md` | D1, D3 | native C4 + **mandatory GitHub render gate** + fallback |
| D3 | `diagrams-d3-ci-syntax-validation.md` | — | CI-only, C4-aware, pinned; not in local hook |
| D4 | `diagrams-d4-coverage-additions.md` | D1, D2, D3 | coverage standard + deploy/CI-CD/session diagrams |
| D5 | `diagrams-d5-adr-docs.md` | D1–D4 | ADR + `decisions.md` + `phases.md` |

**Sequencing:** D1 → D3 → D2 → D4 → D5 (D3 before D2 so C4 blocks are syntax-validated
as they land; D2's GitHub render gate stays authoritative over the validator).
