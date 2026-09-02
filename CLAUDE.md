# OpenGate — Project Rules Index

This file is a one-page index. Each rule lives in its own focused file under [`.claude/rules/`](.claude/rules/). MANDATORY rules are enforced deterministically by [`.claude/hooks/`](.claude/hooks/) — **no bypass mechanism exists**.

## Project State — Read Before Starting Work

**MANDATORY.** Read these three files at session start:

- [`.claude/phases.md`](.claude/phases.md) — **ledger**: what shipped, in what order, linking the plan and the ADRs
- [`.claude/techdebt.md`](.claude/techdebt.md) — **register**: what is still owed, by severity
- [`.claude/decisions.md`](.claude/decisions.md) — **index**: number → one line → phase → status → link (full ADRs in [`docs/adr/`](docs/adr/))

**The ADR is the only home of a decision and its why.** Those three files are
pointers with just enough text to choose a link — a `decisions.md` row is capped
at 200 characters of prose, a `phases.md` row at 300, both enforced by
[`state-index-density.test.sh`](scripts/tests/state-index-density.test.sh).
Rationale goes in the ADR, once.

Canonical developer docs live in [`docs/`](docs/), split into three trees —
[`docs/product/`](docs/product/) (what the system does),
[`docs/architecture/`](docs/architecture/) (how it is built) and
[`docs/infrastructure/`](docs/infrastructure/) (how it runs). Start at
[`docs/Home.md`](docs/Home.md). Read [`docs/README.md`](docs/README.md) before
editing any doc.

After completing significant work, update [`phases.md`](.claude/phases.md), [`techdebt.md`](.claude/techdebt.md), and (for architectural decisions) add an ADR file in [`docs/adr/`](docs/adr/) plus an index row in [`decisions.md`](.claude/decisions.md). Per-file ADRs (013+) are mutable — edit to keep current; supersede only for decision changes.

## Workflow Rules

| Rule | Concern | Enforced by |
|---|---|---|
| [`rules/git.md`](.claude/rules/git.md) | branching (`dev` only), identity, commits, push | `pretooluse-git-commit-guard.sh`, `pretooluse-git-push-guard.sh` |
| [`rules/tdd.md`](.claude/rules/tdd.md) | write failing test before source code | `pretooluse-tdd-gate.sh`, `pretooluse-bash-source-write-guard.sh` |
| [`rules/tests-determinism.md`](.claude/rules/tests-determinism.md) | tests always run — no silent skips (Go/web/Rust) | `pretooluse-test-skip-guard.sh` |
| [`rules/precommit-refactor.md`](.claude/rules/precommit-refactor.md) | `/precommit` before commit; `/refactor` before push | commit/push guards via marker files |
| [`rules/sonarcloud.md`](.claude/rules/sonarcloud.md) | quality-gate workflow; no suppressions without approval | `pretooluse-write-guard.sh` |
| [`rules/coverage-exclusions.md`](.claude/rules/coverage-exclusions.md) | exclusions/suppressions are a last resort; per-entry justification, no directory globs | `sonar-coverage-exclusion-guard.sh` |
| [`rules/plans-and-adrs.md`](.claude/rules/plans-and-adrs.md) | plans location, ADR mutability + archived-plan-link rule | `pretooluse-write-guard.sh` |
| [`rules/cache-hygiene.md`](.claude/rules/cache-hygiene.md) | reclaim local build caches after every push | `post-push-clean-caches.sh`, `posttooluse-cache-clean.sh` |
| [`rules/ci-cd-determinism.md`](.claude/rules/ci-cd-determinism.md) | a CI/CD step whose work was refused must not report success | `ci-cd-determinism.test.sh`, `assert-cache-written.sh` |
| [`rules/docs-live-state.md`](.claude/rules/docs-live-state.md) | docs and comments describe live state only; the three-tree seam | `docs-live-state.test.sh`, `docs-seam.test.sh` |
| [`rules/resource-conservation.md`](.claude/rules/resource-conservation.md) | a completed operation gives back what it took; a counter is not a measurement | `conservation_test.go`, `hijacked-request-context.yaml` |

## Code and Process Conventions

- [`rules/code.md`](.claude/rules/code.md) — Rust / Go / TypeScript conventions + wire protocol
- [`rules/cross-agent.md`](.claude/rules/cross-agent.md) — shared entry point, skills, hooks, and client-specific configuration
- [`rules/editing-and-scope.md`](.claude/rules/editing-and-scope.md) — numbered-list edit protocol, no silent SKIP, zero-manual-install, audit/refactor scope, `/docs` is canonical
- [`rules/tooling.md`](.claude/rules/tooling.md) — `make` targets, `make e2e` rule, past lessons

## Quick Reference

The CLI tools the hooks enforce:

- `/precommit` — runs lints, tests, coverage, docs checks. Writes marker `.claude/.markers/precommit.head`.
- `/refactor` — post-commit refactoring. Writes marker `.claude/.markers/refactor.head`.

Editing [`.claude/settings.json`](.claude/settings.json) is the only way to change hook behavior. No flag, comment, or environment variable bypasses any hook.
