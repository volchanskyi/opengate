# DD-INV — Inventory pass (defines "done" for DD-C and DD-D)

**Parent:** [`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md) (Inventory pass).
**Execution order:** **3rd** (after DD-B, before DD-C/DD-D).
**Status:** Ready. Output is a scratch checklist (not a committed artifact).
**Risk:** None (analysis only) — but skipping it lets DD-C/DD-D miss hits or
over-purge working links.

## Objective

Produce the **complete** per-pattern, per-file inventory. Representative examples
in the master plan are **not** the scope — this enumeration is. It becomes the
DD-C/DD-D work checklist **and** the reviewer's completeness gate.

## Verified current sizing (re-run at execution — these drift)

Scope: `docs/**` (incl. `docs/adr/**` for 013+, **excluding** the frozen
`docs/Architecture-Decision-Records.md`), `.claude/decisions.md`,
`.claude/phases.md`, `.claude/techdebt.md`; plus `server agent web/src` comments
for DD-D.

| Pattern | Grep | Current hits |
|---|---|---|
| M1 dates-in-prose | `\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b\|\b(over the last\|in the past)\b` | ~50 |
| M2 commit/PR IDs | `\bcommit [0-9a-f]{7,40}\b\|\(PR [0-9A-E]\|#[0-9]{3,}` | ~7 |
| M3 phase/PR labels | `\bPhase 1[0-9]\b\|\bPR-[A-E]\b` | ~35 |
| M4 ADR/plan citation tokens | `ADR-[0-9]\|modular-monolith\|plans/` | **~264 — see ⚠** |
| M5 schedules from config | `[0-9]{2}:[0-9]{2} ?UTC\|nightly at` | ~6 |
| Mechanical union (docs+.claude) | — | **~40 files** |
| D code-comment citations | `(//\|/\*\|#).*\b(ADR-[0-9]\|modular-monolith\|Phase 1[0-9]\|PR-[A-E])` | **65 files** (go 42, rs 14, ts 6) |

## ⚠ M4 is a judgment category, not a mechanical purge

The 264 hits are **mostly working ADR links the doctrine wants to KEEP**
("make every reference a working relative link"). Categorize each M4 hit into:

- **KEEP** — already a working `](…/docs/adr/ADR-NNN-….md)` link → leave.
- **LINKIFY** — a bare `ADR-NNN` token in docs prose with no link → make it a
  working link.
- **REPHRASE** — a citation used as logistics ("per ADR-031 PR-C", now ADR-023 Amendment 1) → state the
  decision/behavior directly, optionally with a link.
- **REMOVE** — a `plans/` link to a **non-archived** plan (rots) → repoint to
  `plans/archive/…` or fold into `decisions.md`.

Do **not** report M4 as "264 to fix." Report the KEEP/LINKIFY/REPHRASE/REMOVE
split per file.

## Judgment categories (no clean grep — per-file review)

- **Speculative-future** prose ("subsequent ADR will tighten…", "will eventually…").
- **Negative-state** prose ("there is no SARIF export…"). Seed: grep `no .* (export|support)` then read.
- List every file that plausibly contains these (seed from the M4/negative greps)
  and check each off after review.

## Known drift to fold in (verified this pass)

- CI-Pipeline.md SARIF negative-state moved **:158 → :178**; a **second**
  negative-state+date instance exists at **:250** (SonarCloud removal, 2026-05-29).
- Testing.md `(PR 9)` at **:128**, `03:00 UTC` at **:131**.
- ADR-023 plan links at **17 & 136** (no drift). Infrastructure.md schedule at **:168**.

## Output

A per-pattern, per-file table with counts + the M4 KEEP/LINKIFY/REPHRASE/REMOVE
split + the judgment-category file list. Paste into the DD-C/DD-D PR description or
a scratch note. **"Done"** for C/D = every mechanical hit resolved (or KEEP-justified)
**and** every judgment-category file reviewed off.

## Reviewer checklist

- [ ] All five mechanical greps enumerated per-file (not just counted).
- [ ] M4 split into KEEP/LINKIFY/REPHRASE/REMOVE — not a bulk number.
- [ ] Speculative-future + negative-state files listed for per-file review.
- [ ] Frozen `Architecture-Decision-Records.md` excluded everywhere.

## Done when

The inventory exists and is detailed enough that DD-C/DD-D can execute and a
reviewer can verify completeness against it.
