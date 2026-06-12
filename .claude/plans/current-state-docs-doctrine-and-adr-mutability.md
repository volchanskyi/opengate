# Current-State Docs Doctrine + ADR Mutability Flip

**Status:** Broken into micro-plans (re-evaluated against the live tree). Order
**DD-A → DD-B → DD-INV → DD-C ∥ DD-D → DD-E**:

- [`docs-doctrine-a-governance-flip.md`](archive/docs-doctrine-a-governance-flip.md) — **done** (archived; ADR consolidation folded in); **unblocks teardown [TD6](dormant-scale-out-td6-docs-adrs.md)**
- [`docs-doctrine-b-link-checker.md`](docs-doctrine-b-link-checker.md) — link-enforcement hook
- [`docs-doctrine-inv-inventory-pass.md`](docs-doctrine-inv-inventory-pass.md) — enumerate scope; defines "done" for C/D
- [`docs-doctrine-c-docs-adr-cleanup.md`](docs-doctrine-c-docs-adr-cleanup.md) — docs + ADR-body cleanup (**coordinate with teardown TD6**)
- [`docs-doctrine-d-code-comments.md`](docs-doctrine-d-code-comments.md) — ~65 source files
- [`docs-doctrine-e-diagrams-as-code.md`](docs-doctrine-e-diagrams-as-code.md) — long-term, optional

**Re-evaluation corrections (verified counts/drift):** (1) **M4's ~264
`ADR-[0-9]|plans/` hits are mostly working links the doctrine wants to KEEP** — it
is a judgment category (KEEP/LINKIFY/REPHRASE/REMOVE), **not** 264 noise items to
purge; DD-INV owns the split. (2) Seed line numbers drifted: CI-Pipeline.md SARIF
moved **:158 → :178**, with a **second** negative-state+date instance at **:250**;
Testing.md `(PR 9)`@:128 / `03:00 UTC`@:131; ADR-023 links confirmed @17 & 136;
Infrastructure.md schedule @:168. (3) **ADR-036 is free** (latest = 035); teardown
TD6 only *amends* ADRs, so this plan owns 036. (4) Current mechanical sizing:
M1≈50, M2≈7, M3≈35, M5≈6 across ~40 docs files; DD-D = 65 code files (go 42 / rs 14 / ts 6).

## Context

Maintainers want a "current-state-only" documentation/comment paradigm: strip
chronological/logistical noise (dates-in-prose, PR/commit IDs, phase labels,
short-lived setup tasks, speculative-future notes, "negative-state" descriptions),
make every file/spec reference a working relative link, and fix rotted plan links.
Two governance decisions were taken explicitly:

- **ADRs become MUTABLE** (supersede [ADR-013](../../docs/adr/ADR-013-docs-in-repo-and-immutable-adrs.md)'s
  immutability clause) so ADRs can be corrected in place against current state.
- **Success = content quality, not line count.** Net line delta is an *outcome*;
  removing genuine noise and adding correct clickable links both count as wins.
  (The "net-negative-or-you're-wrong" framing is explicitly **not** the gate — it
  would reward deleting durable rationale, which fights the TDD/coverage/ADR gates.)

All new format rules must be **hook-enforced** (deterministic, no-bypass, <1.5s,
zero-network) to match the existing [hooks](../../.claude/hooks/) posture.

This is large (governance + hooks + every ADR + many docs + ~65 source files), so
it executes as an **ordered sequence of commits**, each passing the full gauntlet.

## The doctrine (precise purge/keep rule — applies everywhere)

**PURGE** (noise): chronological prose ("verified 2026-05-19", "over the last two
weeks"), PR/commit identifiers in prose (`(PR 9)`, "removed in commit 9236826"),
phase/PR labels used as logistics ("Phase 13b PR-E", "the plan's earlier…"),
speculative-future ("subsequent ADR-021/022 will tighten…"), negative-state ("there
is no SARIF export…"), and schedules/values duplicated from config ("nightly at
03:00 UTC" → link to the workflow instead).

**KEEP** (durable): the substantive decision/behavior/why, structural metadata (ADR
`date:`/`status:` frontmatter, table schemas), and any explanation that is the
*only* pointer to non-obvious rationale — rewritten to state it **directly** rather
than by citation. Exported-symbol doc comments are **rewritten, never deleted** (Go
requires them; clippy/eslint/PMAT-TDG grade them).

**Reconciliation of "blanket-purge refs" + "keep the why":** in code comments,
remove every `ADR-NNN §x` / plan / phase / PR **token**, and rephrase the comment to
describe the behavior/why directly. Fully delete a comment only when it was *purely*
a citation or *purely* speculative-future with no current-state content.

## Workstream A — ADR mutability governance flip (gating; do first)

One coherent commit; after it, ADRs are editable and the link rule changes.

- **New ADR** `docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md`: supersedes
  *only* ADR-013's immutability clause (docs-in-repo + wiki-deprecated stand).
  Records, explicitly: **(1)** per-file ADRs (013+) are mutable / cross-checked
  against current state; **(2)** the combined historical log
  `Architecture-Decision-Records.md` (ADR-001–012) **remains frozen** — mutability
  does not reach it; **(3)** the current-state doctrine above, including that
  **success = content quality; net line delta is explicitly NOT a gate** (a line
  budget would reward deleting durable rationale and fight the coverage/ADR/PMAT
  gates) — the real success signal is the Workstream-B link-checker + the
  inventory-hit greps trending to ~0; **(4)** plan links allowed **only** to
  `plans/archive/…`.
- **Hook** [`pretooluse-write-guard.sh`](../../.claude/hooks/pretooluse-write-guard.sh):
  remove the `adr-immutable` block (lines ~35-40); change `adr-plan-link` from
  "block all plan links in ADRs" to "block links to **non-archived** plans"
  (allow `](…/plans/archive/….md)`, block `](…/plans/<other>….md)`) — this
  deterministically enforces directive #5 for every Write/Edit, ADR or doc.
- **Hook tests** [`scripts/tests/hooks.test.sh`](../../scripts/tests/hooks.test.sh):
  flip the ADR-immutability cases to assert edits are now *allowed*; add cases for
  the archived-vs-active plan-link distinction.
- **Rules/index + canonical docs**: rewrite the ADR sections of
  [`plans-and-adrs.md`](../../.claude/rules/plans-and-adrs.md) and
  [`editing-and-scope.md`](../../.claude/rules/editing-and-scope.md); **rewrite
  [`docs/README.md`](../../docs/README.md) §2 "ADRs are immutable"** to the new
  regime (per-file ADRs 013+ are mutable + cross-checked; supersession still used for
  genuine decision *changes*; the 001–012 historical log **stays frozen** — keep that
  statement and the directory-layout note); update [`CLAUDE.md`](../../CLAUDE.md) row
  + the wiki-audit/precommit skills that cite immutability; add the ADR-036 index row
  to [`decisions.md`](../../.claude/decisions.md).

## Workstream B — deterministic link enforcement (Go-native, zero-network)

Implements "high-density clickable linking" + "only archived plan links" + "no
broken anchors" as a hook (the brainstorm's Vector-E conclusion).

- New `scripts/check-doc-links/` Go package: walk `docs/**`, `.claude/**`, ADRs;
  for each `](relative/path…)` verify
  the target file (and `#Lnn`/anchor) exists; flag links into non-archived
  `.claude/plans/`. Internal-only, no network. Wire into the precommit gauntlet
  ([`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh)) and a
  `PreToolUse` doc-write hook. TDD: fixtures for ok/broken/active-plan-link.

## Inventory pass (do **before** C and D — defines "done")

Representative examples are **not** the scope; the **complete inventory is**. Produce
it first and treat it as the work checklist + the reviewer's completeness gate.

1. **Mechanical patterns (grep-complete — fix every hit):**
   - dates-in-prose — `grep -rInE '\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b|\b(over the last|in the past)\b'`
   - commit/PR identifiers in prose — `grep -rInE '\bcommit [0-9a-f]{7,40}\b|\(PR [0-9A-E]|#[0-9]{3,}'`
   - phase/PR labels as logistics — `grep -rInE '\bPhase 1[0-9]\b|\bPR-[A-E]\b'`
   - ADR/plan citation tokens — `grep -rInE 'ADR-[0-9]|modular-monolith|plans/'`
   - schedules duplicated from config — `grep -rInE '[0-9]{2}:[0-9]{2} ?UTC|nightly at'`
   Scope: `docs/**` (incl. `docs/adr/**`, **excluding** the frozen
   `docs/Architecture-Decision-Records.md`), `.claude/decisions.md`,
   `.claude/phases.md`, `.claude/techdebt.md`; and for Workstream D, comments in
   `server agent web/src`.
2. **Judgment categories (per-file review — no clean grep):** speculative-future and
   negative-state prose. List every file that plausibly contains them (seed from the
   ADR/negative greps) and check each off after review — grep does **not** find these.
3. **Output:** a per-pattern, per-file table with counts (paste into the PR
   description or a scratch note — not a committed artifact). "Done" = every
   mechanical hit resolved **and** every judgment-category file reviewed.

**Out of scope (frozen):** `docs/Architecture-Decision-Records.md` (ADR-001–012) stays
frozen per ADR-013 / docs/README.md — do **not** purge or edit it. (This drops the
previously-planned SARIF-sentence edit to that file.)

## Workstream C — documentation + ADR-body cleanup (apply the doctrine to the full inventory)

Scope = **every** inventory hit in `docs/**` (incl. `docs/adr/**` for ADR-013+),
`.claude/decisions.md`, and `.claude/phases.md`. The items below are the first
batches / worked examples, **not** the limit.

- **ADR bodies (013+) — mechanical-noise purge WITH the reconciliation guardrail.**
  Per the maintainer decision, strip dates-in-prose, commit/PR IDs, and phase/PR
  tokens from ADR *bodies* too — but **rewrite to preserve the fact + the why; never
  delete substantive rationale**, and **KEEP** the `date:`/`status:`/`supersedes:`
  frontmatter (structural metadata + the audit trail, with git history as backstop).
  Worked example (ADR-013 §SARIF): "SARIF export was removed in commit 9236826 (the
  dismissed-fingerprint bug made new alerts invisible)" → "SARIF export was removed
  because the dismissed-fingerprint bug made new alerts invisible" — drops the SHA
  *token*, keeps the behavior + reason. The frozen 001–012 log is **excluded** (see
  Inventory).
- **Broken plan links** — fix ADR-023 lines 17 & 136: repoint to
  `plans/archive/modular-monolith-evaluation.md` (now editable post-A), or fold the
  pointer into [`decisions.md`](../../.claude/decisions.md).
- **Testing.md**: link the bare-text targets — `auth.spec.ts`,
  `security-permissions.spec.ts`, `ErrorBoundary.test.tsx`, `api-baseline.js`
  (resolve real paths via `find`), drop `(PR 9)` from the heading, replace
  "at 03:00 UTC" with the [`mutation.yml`](../../.github/workflows/mutation.yml) link.
- **Negative-state purge**: remove the "no SARIF export…" sentence in
  [`CI-Pipeline.md`](../../docs/CI-Pipeline.md):158. (The analogous sentence in the
  frozen `Architecture-Decision-Records.md` is **left as-is** — that file is frozen.)
- **Schedule purge**: [`Infrastructure.md`](../../docs/Infrastructure.md):168
  "nightly at 03:00 UTC" → link `terraform-drift.yml`, drop the time.
- **decisions.md / phases.md**: strip chronological prose + PR/phase logistics from
  free-text cells; keep the structural table + the decision substance. Add
  rationale-trace links where useful (e.g. the ADR-014→ADR-003 supersession note —
  as a working relative link, per the directive's encouraged pattern).

## Workstream D — code-comment purge (~65 source files)

Apply the reconciliation rule above to **every** file in the inventory (not just the
seed examples below). Batch by module/language so each commit is reviewable and keeps
the gauntlet green. Seed examples:
[`server/internal/usecase/session.go`](../../server/internal/usecase/session.go),
[`server/internal/amt/repository.go`](../../server/internal/amt/repository.go),
[`server/internal/db/models.go`](../../server/internal/db/models.go),
[`server/internal/audit/handlers_test.go`](../../server/internal/audit/handlers_test.go).
Find the set: `grep -rIlnE '(//|/\*|#).*\b(ADR-[0-9]|modular-monolith|Phase 1[0-9]|PR-[A-E])' server agent web/src`.

Gate handling: comment edits to `*.go/.rs/.ts` are **source edits** → satisfy the
TDD gate by including the in-set `*_test.go`/`*.test.ts` files in the first batch.
Watch PMAT-TDG: rewrite (don't delete) exported-symbol doc comments.

## Workstream E — long-term: Diagram/Docs-as-Code (brainstorm → recommendation)

- **Engine:** **Mermaid fenced blocks**, *not* a D2/SVG pipeline. GitHub renders
  Mermaid server-side — zero CLI, zero Puppeteer/JRE, zero committed SVG blobs (which
  would be a large positive byte/line delta and a drift surface). D2→SVG only earns
  its keep if rendering off-GitHub, which we don't.
- **Drift:** do **not** auto-extract diagrams into docs (AST sprawl + drift). Reuse
  the boundary tools already enforced ([`tooling.md`](../../.claude/rules/tooling.md)):
  `go-arch-lint`, `dependency-cruiser` (boundary-scoped), `cargo-modules`
  (metadata-only — *not* `cargo-expand`, which blows the 1.5s budget). Hand-curate a
  few high-level Mermaid diagrams; let the linters catch structural drift.
- **Links:** Workstream B's Go-native internal checker (over Node
  `markdown-link-check`, which adds a Node boundary + network).
- Net effect: zero new heavy runtimes, all within the <1.5s/zero-network envelope.

## Sequencing (each commit = full gauntlet)

1. **A** governance flip (ADR-036 + hook + hook tests + rules + CLAUDE.md +
   **docs/README.md** + index).
2. **B** link-checker hook (+ TDD fixtures) — so C/D are enforced as they land.
3. **Inventory pass** — enumerate every mechanical hit + flag every judgment-category
   file; this list is the C/D checklist and the reviewer's "done" definition.
4. **C** docs + ADR-body cleanup over the **full inventory** (ADR-023 links first,
   then Testing.md, then docs schedule/negative-state purge, then ADR 013+ bodies,
   then decisions.md/phases.md). The frozen 001–012 log is excluded.
5. **D** code comments, batched by module, over the full inventory.
6. Update [`techdebt.md`](../../.claude/techdebt.md)/[`phases.md`](../../.claude/phases.md);
   archive this plan.

## Risks

- **Governance reversal is real**: mutable ADRs lose the immutable audit trail; the
  supersede-ADR-036 + the `date:`/`status:` frontmatter + git history are the
  mitigation. Carving ADR-013 (3 bundled decisions) wrong would also rescind
  docs-in-repo/wiki — the ADR-036 text must scope to the immutability clause only.
- **PMAT-TDG / Go-lint**: over-zealous comment deletion drops exported-symbol docs →
  lint/grade failure. Rule: rewrite, don't delete.
- **Doctrine vs. ADR nature**: ADRs are inherently dated decision records, so the
  maintainer decision to purge mechanical noise from ADR *bodies* (013+) is the
  highest-risk part of this plan. The guardrail against gutting the knowledge system
  is the reconciliation rule: **rewrite to preserve the fact + the why, never delete
  substantive rationale, and KEEP `date:`/`status:`/`supersedes:` frontmatter**
  (structural metadata + audit trail, with git history as backstop). Purging a SHA or
  date *token* must retain the behavior and the reason it records. The combined
  historical log (`Architecture-Decision-Records.md`, 001–012) stays **frozen** and
  out of scope.
- **Breadth**: ~65 code files + every ADR + many docs cannot be one commit; staged
  commits keep each gauntlet green and reviewable.

## Verification

- **Success signal = the deterministic checks below, NOT net line delta** (recorded
  in ADR-036; net delta is an outcome, never a gate).
- New link-checker green across `docs/**` + `.claude/**` (no broken/active-plan links).
- Full gauntlet green per commit (lints, tests, coverage, PMAT-TDG ≥ B+, sonar).
- Hook-test suite green incl. the new mutable-ADR + archived-plan-link cases; a
  manual Edit of an ADR is now *allowed*, a link to an active plan is *blocked*.
- Spot-check rendered Mermaid (if added) on a GitHub preview; spot-check the four
  Testing.md links resolve.
- **Every inventory hit resolved:** the mechanical-pattern greps return ~0 across the
  scoped dirs (`server agent web/src` comments; `docs/**` **excl.** the frozen
  `Architecture-Decision-Records.md`, plus `.claude/decisions.md`/`phases.md`), and
  every judgment-category file in the inventory is reviewed off.
