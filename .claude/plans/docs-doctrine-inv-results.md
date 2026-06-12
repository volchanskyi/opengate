# DD-INV results — current-state doctrine inventory (DD-C / DD-D checklist)

Scratch working-checklist produced by the DD-INV pass. Defines "done" for DD-C
(docs + ADR bodies) and DD-D (code comments). Re-run the greps at execution —
counts drift. Plan refs below are inline-code (not links) on purpose.

**Scope (41 files):** `docs/**/*.md` EXCLUDING the frozen
`docs/Architecture-Decision-Records.md`, plus `.claude/decisions.md`,
`.claude/phases.md`, `.claude/techdebt.md`. (`.claude/plans/**` is out of scope —
plans are working notes.)

## Mechanical patterns (grep-complete — fix every hit)

| Pattern | Grep | Lines | Files |
|---|---|---|---|
| M1 dates-in-prose | `\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b|over the last|in the past` | 52 | 26 |
| M2 commit/PR IDs | `commit [0-9a-f]{7,40}|\(PR [0-9A-E]|#[0-9]{3,}` | 9 | 6 |
| M3 phase/PR labels | `Phase 1[0-9]|PR-[A-E]` | 30 | 9 |
| M4 ADR/plan tokens | `ADR-[0-9]|modular-monolith|plans/` | 254 | 33 |
| M5 schedules from config | `[0-9]{2}:[0-9]{2} ?UTC|nightly at` | 6 | 3 |

### M1 dates-in-prose (52 lines / 26 files) — top files
ADR-023 (5), ADR-020 (4), phases.md (4), ADR-036 (3), ADR-027 (3), ADR-019 (3),
ADR-013 (3), techdebt.md (3), ADR-030/024/022/016/014 (2 each), Multiscale (2),
+ 11 files with 1. **Self-introduced today:** the Amendment dates `(2026-05-31)`
etc. in ADR-019/020/023 and ADR-036's Status date — see "Decision needed" below.

### M2 commit/PR identifiers (9 / 6)
ADR-036 (2), Monitoring.md (2), phases.md (2), ADR-013 (1), Testing.md (1),
README.md (1). Pattern: `commit 9236826`-style SHAs and `(PR 9)` — strip the
token, keep the behavior+reason (ADR-013 §SARIF is the worked example).

### M3 phase/PR labels (30 / 9)
ADR-030 (6), phases.md (6), ADR-034 (5), ADR-023 (3), Kubernetes-Migration (3),
ADR-014 (2), Testing (2), decisions.md (2), Architecture (1). "Phase 13b PR-C"
→ state the behavior directly.

### M5 schedules from config (6 / 3)
phases.md (4), Testing.md (1) `03:00 UTC`, Infrastructure.md (1) `nightly at` →
link the workflow instead of restating the time.

## M4 — judgment split (254 lines / 33 files): KEEP / LINKIFY / REPHRASE / REMOVE

- **KEEP ≈ 130** working links: 58 `](…/ADR-NNN-….md)` ADR links + 72
  `](…/plans/archive/….md)` archived-plan links. Leave as-is.
- **LINKIFY** — bare `ADR-NNN` tokens in prose with no link. Top docs files:
  Multiscale (14), README (5), ADR-014 (5), ADR-019 (4), Kubernetes-Migration (4),
  ADR-013 (3), ADR-034/030/023 (2). NOTE: `decisions.md` (12) and `phases.md` (17)
  bare tokens are mostly the index/record structure — KEEP-as-structure, not
  LINKIFY. Review per line.
- **REPHRASE** — ADR/phase token used as logistics; state the decision directly.
  Overlaps M3.
- **REMOVE / repoint** — plan links the doctrine forbids (active) or that rot:
  - **ACTIVE plan (fold or de-link)** — `fast-path-reconnect-fix.md`:
    `docs/Multiscale-Readiness.md:100,134,309`, `.claude/techdebt.md:85`.
  - **ROTTED (archived plan linked via pre-archive path → repoint to
    `plans/archive/…`)**:
    - `docs/adr/ADR-018:79` → `stable-dev-machine-vps-access.md`
    - `docs/Testing.md:162` → `pr9-mutation-testing-as-observability.md`
    - `docs/adr/ADR-024:98`, `ADR-021:10,96`, `ADR-022:20,110` →
      `modular-monolith-evaluation.md`
    - `docs/adr/ADR-016:5` → `tests-coverage-phase-c-structural-hardening.md`
    - `.claude/phases.md:63` → `tests-coverage-phase-b-coverage-depth.md`
    - `.claude/phases.md:107` → `performance-benchmarks.md`

  These REMOVE/rotted lines are already in the DD-B baseline
  (`.claude/doc-link-baseline.txt`); fixing them shrinks the baseline.

## Judgment categories (per-file review — no clean grep)

- **Negative-state prose** (seed `there is no|no … export/support|not
  implemented|no longer`, ~19 lines / 10 files to review): ADR-036 (3),
  ADR-017 (3), Database.md (3), Multiscale (2), techdebt.md (2), phases.md (2),
  ADR-035 (1), Infrastructure.md (1), Continuous-Deployment.md (1),
  CI-Pipeline.md (1). The CI-Pipeline SARIF sentence is the canonical purge.
- **Speculative-future prose** (seed `subsequent ADR|eventually|in a future|will
  tighten/land/follow/introduce`, 4 lines / 4 files): ADR-036 (1), ADR-021 (1),
  ADR-020 (1), README.md (1).

## DD-D — code-comment citations (server agent web/src)

`grep -rIlnE '(//|/\*|#).*\b(ADR-[0-9]|modular-monolith|Phase 1[0-9]|PR-[A-E])'`
→ **65 files, 126 lines.** By language: **Go 42, Rust 14, TS 6**, plus 3 config
files with `#` comments (`agent/deny.toml`, `server/.go-arch-lint.yml`,
`agent/.cargo/mutants.toml`). Reconciliation rule: remove the token, rephrase to
the behavior/why; rewrite (never delete) exported-symbol doc comments; batch by
module so each commit keeps the gauntlet green; include in-set `*_test.go` /
`*.test.ts` in the first batch to satisfy the TDD gate.

## Decision needed (flag to maintainer before DD-C edits ADR bodies)

**Amendment dates as audit metadata.** Today's consolidation added in-prose dates
to ADR-019/020/023 Amendments (`(2026-05-31)` …) and ADR-036's Status. These are
M1 hits, but they are the audit trail that ADR-036 itself relies on for mutable
ADRs (analogous to `date:` frontmatter). Recommend **KEEP** amendment/decision
dates; purge only incidental chronological prose. Confirm before DD-C strips them.

## "Done" for DD-C / DD-D
Every mechanical hit (M1/M2/M3/M5 + M4 REMOVE/LINKIFY) resolved or KEEP-justified,
every negative-state + speculative-future file reviewed off, and the DD-B doc-link
gate green with a shrunken baseline. Frozen `Architecture-Decision-Records.md`
untouched throughout.
