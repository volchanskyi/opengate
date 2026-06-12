# OpenGate — Project Rules Index

This file is a one-page index. Each rule lives in its own focused file under [`.claude/rules/`](.claude/rules/). MANDATORY rules are enforced deterministically by [`.claude/hooks/`](.claude/hooks/) — **no bypass mechanism exists**.

## Project State — Read Before Starting Work

**MANDATORY.** Read these three files at session start:

- [`.claude/phases.md`](.claude/phases.md) — completed / in-progress / planned phases
- [`.claude/techdebt.md`](.claude/techdebt.md) — known tech debt by severity
- [`.claude/decisions.md`](.claude/decisions.md) — ADR index (full ADRs in [`docs/adr/`](docs/adr/))

Canonical developer docs live in [`docs/`](docs/). Start at [`docs/Home.md`](docs/Home.md). Read [`docs/README.md`](docs/README.md) before editing any doc.

After completing significant work, update [`phases.md`](.claude/phases.md), [`techdebt.md`](.claude/techdebt.md), and (for architectural decisions) add an ADR file in [`docs/adr/`](docs/adr/) plus an index row in [`decisions.md`](.claude/decisions.md). Per-file ADRs (013+) are mutable — edit to keep current; supersede only for decision changes.

## Workflow Rules

| Rule | Concern | Enforced by |
|---|---|---|
| [`rules/git.md`](.claude/rules/git.md) | branching (`dev` only), identity, commits, push | `pretooluse-git-commit-guard.sh`, `pretooluse-git-push-guard.sh` |
| [`rules/tdd.md`](.claude/rules/tdd.md) | write failing test before source code | `pretooluse-tdd-gate.sh`, `pretooluse-bash-source-write-guard.sh` |
| [`rules/tests-determinism.md`](.claude/rules/tests-determinism.md) | tests always run — no silent skips (Go/web/Rust) | `pretooluse-test-skip-guard.sh` |
| [`rules/precommit-refactor.md`](.claude/rules/precommit-refactor.md) | `/precommit` before commit; `/refactor` before push | commit/push guards via marker files |
| [`rules/sonarcloud.md`](.claude/rules/sonarcloud.md) | quality-gate workflow; no suppressions without approval | `pretooluse-write-guard.sh` |
| [`rules/plans-and-adrs.md`](.claude/rules/plans-and-adrs.md) | plans location, ADR mutability + archived-plan-link rule | `pretooluse-write-guard.sh` |

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
