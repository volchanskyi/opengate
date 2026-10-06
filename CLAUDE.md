# OpenGate — Project Rules Index

This file is a one-page index. Each rule lives in its own focused file under
[`.claude/rules/`](.claude/rules/). MANDATORY rules are enforced
deterministically by [`.claude/hooks/`](.claude/hooks/) — **no bypass mechanism
exists**.

## Project State — Read Before Starting Work

**MANDATORY.** Read these three files at session start:

- [`.claude/phases.md`](.claude/phases.md) — **ledger**: what the system is made
  of, in the order it arrived, linking the ADRs
- [`.claude/techdebt.md`](.claude/techdebt.md) — **register**: what is still
  owed, by severity
- [`.claude/decisions.md`](.claude/decisions.md) — **index**: number → one line →
  link (full ADRs in [`docs/adr/`](docs/adr/))

Rules for those three files:

- The ADR is the only home of a decision and its why. Rationale goes in the ADR,
  once.
- The three files are pointers with just enough text to choose a link. A
  `decisions.md` row is capped at 200 characters of prose, a `phases.md` row at
  300, both enforced by
  [`state-index-density.test.sh`](scripts/tests/state-index-density.test.sh).

Rules for documentation:

- Canonical developer docs live in [`docs/`](docs/), split into three trees:
  [`docs/product/`](docs/product/) (what the system does),
  [`docs/architecture/`](docs/architecture/) (how it is built) and
  [`docs/infrastructure/`](docs/infrastructure/) (how it runs).
- Start at [`docs/Home.md`](docs/Home.md).
- Read [`docs/README.md`](docs/README.md) before editing any doc.

After completing significant work:

- Update [`phases.md`](.claude/phases.md) and
  [`techdebt.md`](.claude/techdebt.md).
- **Never add a techdebt entry without asking first.** Resolve what you find.
  Where it truly cannot be resolved, ask the user before writing it, with a
  summary in plain language — no jargon
  ([`plans-and-adrs.md`](.claude/rules/plans-and-adrs.md#adding-a-techdebt-entry)).
- Delete the plan.
- For an architectural decision, add an ADR file in [`docs/adr/`](docs/adr/)
  plus an index row in [`decisions.md`](.claude/decisions.md).
- Every ADR describes live state: edit it in place when the decision changes,
  delete it when the decision leaves nothing behind, and fold a minor fix into
  the ADR it refines rather than writing a new one.

## Workflow Rules

| Rule | Concern | Enforced by |
|---|---|---|
| [`rules/git.md`](.claude/rules/git.md) | branching (`dev` only), identity, commits, push | `pretooluse-git-commit-guard.sh`, `pretooluse-git-push-guard.sh` |
| [`rules/tdd.md`](.claude/rules/tdd.md) | write failing test before source code | `pretooluse-tdd-gate.sh`, `pretooluse-bash-source-write-guard.sh` |
| [`rules/tests-determinism.md`](.claude/rules/tests-determinism.md) | tests always run — no silent skips (Go/web/Rust) | `pretooluse-test-skip-guard.sh` |
| [`rules/test-value.md`](.claude/rules/test-value.md) | a test asserts on the code that ships, and restores what it patches | `pretooluse-test-value-guard.sh`, `test-value.test.sh` |
| [`rules/assertion-determinism.md`](.claude/rules/assertion-determinism.md) | an assertion is not a pipeline — a match lost to `SIGPIPE` reads as a pass | `pipefail-sigpipe.test.sh` |
| [`rules/refactor.md`](.claude/rules/refactor.md) | the gauntlet passes, then `/refactor`, then the commit, which runs the gauntlet again | `refactor-gate.sh`; commit guard checks the tidy-up, then runs the gauntlet; push guard reads the marker |
| [`rules/sonarcloud.md`](.claude/rules/sonarcloud.md) | quality-gate workflow; no suppressions without approval | `pretooluse-write-guard.sh` |
| [`rules/coverage-exclusions.md`](.claude/rules/coverage-exclusions.md) | exclusions/suppressions are a last resort; per-entry justification, no directory globs | `sonar-coverage-exclusion-guard.sh` |
| [`rules/plans-and-adrs.md`](.claude/rules/plans-and-adrs.md) | plans location, deleting a plan when its work lands, ADRs as live state | `pretooluse-write-guard.sh` |
| [`rules/tool-versions.md`](.claude/rules/tool-versions.md) | one version, written down once — local and CI provision from the same manifest, and a typed install names the pin | `tool-version-parity.test.sh`, `toolchain-parity.sh`, `pretooluse-tool-install-guard.sh` |
| [`rules/cache-hygiene.md`](.claude/rules/cache-hygiene.md) | reclaim local build caches after every push | `post-push-clean-caches.sh`, `posttooluse-cache-clean.sh` |
| [`rules/ci-cd-determinism.md`](.claude/rules/ci-cd-determinism.md) | a CI/CD step whose work was refused must not report success | `ci-cd-determinism.test.sh`, `alert-delivery.test.sh`, `assert-cache-written.sh` |
| [`rules/docs-live-state.md`](.claude/rules/docs-live-state.md) | docs describe live state only; the three-tree seam | `docs-live-state.test.sh`, `docs-seam.test.sh` |
| [`rules/code-comments.md`](.claude/rules/code-comments.md) | every comment is a short positive statement about the code it sits on | `pretooluse-comment-check.sh`, `check-comments.test.sh` |
| [`rules/resource-conservation.md`](.claude/rules/resource-conservation.md) | a completed operation gives back what it took; a counter is not a measurement | `conservation_test.go`, `hijacked-request-context.yaml` |

## Code and Process Conventions

- [`rules/code.md`](.claude/rules/code.md) — Rust / Go / TypeScript conventions + wire protocol
- [`rules/cross-agent.md`](.claude/rules/cross-agent.md) — shared entry point, skills, hooks, and client-specific configuration
- [`rules/editing-and-scope.md`](.claude/rules/editing-and-scope.md) — numbered-list edit protocol, no silent SKIP, zero-manual-install, audit/refactor scope, `/docs` is canonical
- [`rules/tooling.md`](.claude/rules/tooling.md) — `make` targets, `make e2e` rule, past lessons

## Quick Reference

- The order a commit takes: `./scripts/precommit-gauntlet.sh` until it passes →
  `/refactor` → commit. The commit guard refuses unless `/refactor` finished on
  exactly the content on disk, then runs the gauntlet again — lints, tests,
  coverage, audits, benchmarks, e2e, sonar. There is no bypass.
- `/refactor` — tidy-up of the change before it is committed; refuses to start
  without a gauntlet pass on the content on disk.
- The post-commit hook pushes a commit made from tidied content and writes
  `.claude/.markers/refactor.head`, which the push guard reads. No marker is
  written by hand.

Editing [`.claude/settings.json`](.claude/settings.json) is the only way to
change hook behavior. No flag, comment, or environment variable bypasses any
hook.
