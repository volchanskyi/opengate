# OpenGate — Working Conventions

This file captures project-specific lessons, shortcuts, and worked examples that don't belong in [CLAUDE.md](../CLAUDE.md) (the mandatory rules) or in [.claude/phases.md](phases.md) / [.claude/techdebt.md](techdebt.md) / [.claude/decisions.md](decisions.md) (state).

CLAUDE.md is the canonical source of truth for non-negotiable rules. This file is the explanatory companion: *why* a rule exists, *how* to apply it on edge cases, and the worked examples that make the rules concrete. Rules in CLAUDE.md are enforced deterministically by `.claude/hooks/` (see PR 2 of the `claude-hooks-port-mandatory-directives` plan). The hooks have **no bypass**.

---

## Workflow rules (canonical text in CLAUDE.md)

| Rule | CLAUDE.md ref | Enforced by |
|---|---|---|
| Work on `dev` only; never commit/push to `main` | §Branching Rules | `pretooluse-git-commit-guard.sh`, `pretooluse-git-push-guard.sh` |
| Author = Ivan Volchanskyi; no `Co-Authored-By` trailers | §Git Identity | `pretooluse-git-commit-guard.sh` |
| TDD: failing test before source code | §TDD Mandate | `pretooluse-tdd-gate.sh` (fires on first source edit per branch) |
| Run `/precommit` before every commit | §Pre-Commit Checklist | `pretooluse-git-commit-guard.sh` via marker file |
| Run `/refactor` after `/precommit` passes | §Post-Commit Refactoring | `pretooluse-git-push-guard.sh` via marker file |
| ADRs in `docs/adr/` are immutable; supersede with a new file | §Project State | `pretooluse-write-guard.sh` |
| No `NOSONAR` / `//nolint` / `sonar.issue.ignore` without explicit user approval | §SonarCloud Workflow | `pretooluse-write-guard.sh` |
| Plans go in `/home/ivan/opengate/.claude/plans/`, not `~/.claude/plans/` | §Project State | `pretooluse-write-guard.sh` |

---

## Tooling shortcuts

- **`make e2e`, not `npx playwright test`.** `make e2e` owns the full Docker Compose lifecycle (`up --build --wait` → `playwright test` → `down -v`). The bare `npx playwright test` invocation relies on Playwright's `webServer` block with a 180s timeout that is too short for cold Docker builds; tests fail before the stack is ready. This applies inside `/precommit` and anywhere else E2E tests run.
- **`/precommit` after every PR in a multi-PR rollout** — including docs-only PRs. Security audits (`govulncheck`, `cargo audit`, `npm audit`) evaluate lockfiles, not the diff, so a new advisory published yesterday will fail today's gate even if today's diff only touches Markdown. The one exception is a fast-follow commit that fixes a CI failure already reported by a previous push — re-running `/precommit` adds no safety and wastes a CI cycle.
- **`/refactor` after `/precommit` passes** — CLAUDE.md §Post-Commit Refactoring mandates this. The push-guard hook enforces it via a marker file when commits since `origin/dev` touch source files.
- **Plan files live in the project, not the user-global dir.** Plan mode's auto-suggested path under `~/.claude/plans/` is wrong; ignore it. Always write to `/home/ivan/opengate/.claude/plans/<kebab-case>.md`. The write-guard hook blocks any other location.

---

## Editing protocol

- **Read the whole file before editing numbered or globally-ordered structures.** Numbered lists, ADR indexes, phases tables, OpenAPI parameter orderings, migration files, changelog entries — these are silent invariants. A partial insert that doesn't renumber the rest rots cross-references elsewhere in the file (e.g. "see step 17" prose downstream). Before editing such a structure: `Read` the whole file, then `grep` for ordinal cross-references (`step [0-9]+`, `step #[0-9]+`, `section [0-9]\.[0-9]`, `phase [A-Z]:`) and consolidate the renumber into one Edit/Write.
- **Never claim "SKIP" passes in `/precommit`.** A pre-commit step that exits 0 because the underlying tool isn't on `$PATH` is a setup defect, not a pass — it appears clean locally but fails in CI. If a tool is missing, fail loudly with a clear setup message; don't write `|| echo "SKIP"`.
- **Zero manual installation steps for the agent.** Anything environment-specific (desktop tray, GUI shims, platform-only crates) must auto-detect at install time and silently no-op on unsupported environments. No `--flags`, no separate install scripts, no "also run X on desktop machines" documentation. One install command handles every fleet machine.

---

## Scope rules for plans and audits

- **No operational scripts in refactor/audit plans unless explicitly requested.** Backup scripts, retention jobs, alerting routines, and similar operational tooling are out of scope for codebase audits and refactoring efforts. Propose them separately when a user need surfaces. Hardening phases focus on configuration changes (resource limits, CI gates, alert rules), not new operational scripts.
- **`/docs` is the canonical developer documentation.** Each implementation phase ends with a `/docs` update step. The previous GitHub Wiki is deprecated; do not edit it.

---

## TDD lifecycle — worked examples

The TDD gate fires only on the **first source-file edit per branch**. Once a test file change exists anywhere on the branch (committed, staged, unstaged, or untracked), the gate is silent for the rest of the branch's life.

### New feature

```
1. git checkout dev && git pull --rebase origin dev
2. Edit  server/internal/api/handlers_test.go     # add failing test
3. Edit  server/internal/api/handlers.go          # gate now silent — test exists on branch
4. (iterate)
5. /precommit                                     # writes marker
6. git commit
7. /refactor                                       # writes marker
8. /precommit                                     # re-validate
9. git commit
10. git push origin dev
```

### Bug fix

```
1. Edit  server/internal/api/handlers_test.go    # add failing regression test
2. Edit  server/internal/api/handlers.go         # fix; gate silent
3. /precommit → commit → /refactor → /precommit → commit → push
```

### Pure refactor

CLAUDE.md §TDD step 3 calls for a refactor only after tests are green. To touch the source, *first* touch the covering test — strengthen an assertion, or add a `// covers …` annotation that exercises the behavior the refactor preserves. This costs almost nothing and keeps the discipline.

```
1. Edit  server/internal/relay/relay_test.go     # strengthen assertion / add covers comment
2. Edit  server/internal/relay/relay.go          # refactor; gate silent
3. (run tests; confirm green)
4. /precommit → commit → /refactor → /precommit → commit → push
```

### Generated code

`openapi_gen.go`, files matching `*_gen.go`, and `*.pb.go` are excluded from the source classifier. Running the generator via `Bash` (e.g. `oapi-codegen`) and committing the output requires no prior test edit. Hand-editing generated files is discouraged for other reasons — they will be overwritten on the next regeneration.

---

## Past lessons

- **`govulncheck` vs `$HOME/go` install:** never extract a Go tarball to `$HOME/go`. That path is the default `GOPATH` when `GOPATH` is unset, so the toolchain ends up with two copies of stdlib and `govulncheck` errors with "redeclared in this block" against `$HOME/go/src/net/*.go`. The convention is a snap- or apt-managed `go` binary on `$PATH` plus `GOPATH=$HOME/go-workspace` exported in `~/.bashrc`. See the prerequisites block in `.claude/skills/precommit/SKILL.md`.
- **SonarCloud failures fetch all three endpoints first.** On the first quality-gate failure of a session, hit `/api/issues/search`, `/api/hotspots/search`, and `/api/qualitygates/project_status` in parallel — they return disjoint data. The issues endpoint returning `total: 0` does NOT mean the gate is green. Canonical text: CLAUDE.md §SonarCloud Workflow.
