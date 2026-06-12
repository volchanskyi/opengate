# DD-D — Code-comment purge (~65 source files)

**Parent:** [`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md) (Workstream D).
**Execution order:** **5th** (after DD-INV; may run in parallel with DD-C — disjoint files).
**Status:** Ready.
**Risk:** PMAT-TDG / Go-lint regression from over-zealous deletion. **Rule:
rewrite, don't delete; exported-symbol doc comments are rewritten, never removed.**

## Objective

Remove citation **tokens** (`ADR-NNN §x`, `modular-monolith`, `Phase 1x`, `PR-x`)
from code comments and rephrase each comment to state the behavior/why **directly**.
Fully delete a comment only when it was *purely* a citation or *purely*
speculative-future with no current-state content. This is the on-the-ground
application of the existing rule
[`feedback-no-adr-citations-in-code-comments`] — see [`code.md`](../rules/code.md)
conventions.

## Verified scope (65 files — re-run at execution)

`grep -rIlnE '(//|/\*|#).*\b(ADR-[0-9]|modular-monolith|Phase 1[0-9]|PR-[A-E])' server agent web/src`

| Language | Files | Test-file pairing for the gate |
|---|---|---|
| Go (`server`) | **42** | include the in-set `*_test.go` in each Go batch |
| Rust (`agent`) | **14** | include the in-set `*_test`/`#[cfg(test)]` file or strengthen a covering test |
| TS (`web/src`) | **6** | include the in-set `*.test.ts(x)` in the TS batch |

## TDD gate handling (important)

Comment edits to `*.go/.rs/.ts` are **source edits** → the TDD gate
(`pretooluse-tdd-gate.sh`) requires a test change on the branch. Satisfy it by
**including the in-set test files in the first batch** (they carry citation
comments too). After one test edit lands on the branch, the gate is silent for
the rest — but keep batching by module for reviewability.

## Batching (each = one commit, gauntlet green)

Batch by module so each commit is small and `go vet`/clippy/eslint + PMAT-TDG
stay green:

1. **Go batch 1** — a cohesive package group incl. its `*_test.go` (seeds:
   [`usecase/session.go`](../../server/internal/usecase/session.go),
   [`amt/repository.go`](../../server/internal/amt/repository.go),
   [`db/models.go`](../../server/internal/db/models.go),
   [`audit/handlers_test.go`](../../server/internal/audit/handlers_test.go)).
2. **Go batches 2..n** — remaining Go files by package.
3. **Rust batch** — the 14 `agent` files.
4. **TS batch** — the 6 `web/src` files.

## Steps per batch

- Pull the batch's files from DD-INV.
- Rewrite each comment per the reconciliation rule; **never** drop an
  exported-symbol doc comment (Go requires it; clippy/eslint/PMAT-TDG grade it).
- `go build && go vet && make lint` (or clippy/eslint) clean; PMAT-TDG ≥ B+.
- `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [ ] Every DD-INV code-comment file resolved; the seed grep returns ~0 in `server agent web/src`.
- [ ] No exported-symbol doc comment deleted (rewritten only); PMAT-TDG grade not regressed.
- [ ] Comments state behavior/why directly — no `ADR-NNN`/`Phase`/`PR-` tokens remain.
- [ ] `go vet`/clippy/eslint clean per batch; full `/precommit` gauntlet green.

## Done when

The code-comment citation grep returns ~0 across `server agent web/src` and no
exported-symbol documentation was lost.
