# Modularize Markdown Guidelines into `.claude/rules/`

## Context

The project's Markdown guideline layer has accreted bloat that will grow worse over time. An audit (2026-05-14) found:

- **`/home/ivan/opengate/AGENT.MD` (122 lines)** is byte-identical to [`CLAUDE.md`](../../CLAUDE.md) (`diff -q` returns nothing). Untracked. The user wants it kept as a symlink so non-Claude tooling that probes for `AGENT.MD` / `AGENTS.md` finds the same content as Claude Code.
- **[`CLAUDE.md`](../../CLAUDE.md) (122 lines)** mixes mandatory workflow rules, code conventions (Rust/Go/TypeScript), wire-protocol notes, and a `make` command list. Each concern is loaded on every session even when irrelevant to the task.
- **[`.claude/conventions.md`](../conventions.md) (95 lines)** opens with a cross-reference table (lines 9–20) restating CLAUDE.md rules verbatim. The remainder (tooling shortcuts, editing protocol, TDD worked examples, past lessons) is unique value-add but mixed in with the duplicated table.
- **8 feedback memory files** in `/home/ivan/.claude/projects/-home-ivan-opengate/memory/` (`feedback_*.md`) were promoted into `conventions.md` per Claude Hooks PR 1, but the upstream copies were never deleted. Each is now a duplicate.
- Session-start payload today (CLAUDE.md + conventions.md + MEMORY.md + linked memories) is ~365 lines. With AGENT.MD also being read by some tools, the burden is asymmetric across the toolchain.

The user has chosen the **skill-style modularization** architecture: split each rule category into its own focused file under `.claude/rules/`, with [`CLAUDE.md`](../../CLAUDE.md) reduced to a one-page index. The choice mirrors the existing [`.claude/skills/<name>/SKILL.md`](../skills/) pattern, scales linearly as new rules are added, and lets a session load only the rule files relevant to its task.

Three additional decisions, per the same review:
- `AGENT.MD` → symbolic link to `CLAUDE.md`, tracked in git so the link mode persists.
- Feedback memories that are now duplicated in repo conventions → delete the upstream copies. Keep originals only where they carry cross-session context not codified in the repo (environment notes, design patterns, references).
- Hook error messages and session-start context-load output point at the new rule files instead of `conventions.md`.

## Approach

### 1. Create `.claude/rules/` with 8 topical files

Each file owns one concern, no overlap. Approximate line counts in parentheses; each file fits on one screen.

| File | Owns | Source content (today) |
|---|---|---|
| [`.claude/rules/git.md`](../rules/git.md) (~25 lines) | Branching (dev-only), git identity, commit & push discipline | CLAUDE.md §Branching Rules + §Git Identity |
| [`.claude/rules/tdd.md`](../rules/tdd.md) (~55 lines) | TDD mandate + lifecycle worked examples (new feature / bug fix / pure refactor / generated code) | CLAUDE.md §TDD Mandate + conventions.md §TDD lifecycle |
| [`.claude/rules/precommit-refactor.md`](../rules/precommit-refactor.md) (~30 lines) | `/precommit` and `/refactor` mandates, marker files, per-PR re-run rule for multi-PR rollouts | CLAUDE.md §Pre-Commit + §Post-Commit + conventions.md tooling shortcuts (precommit-per-PR, refactor-after-precommit) |
| [`.claude/rules/sonarcloud.md`](../rules/sonarcloud.md) (~40 lines) | Quality-gate workflow, fetch-all-three-endpoints, no-suppression policy | CLAUDE.md §SonarCloud Workflow + conventions.md past-lessons §SonarCloud |
| [`.claude/rules/plans-and-adrs.md`](../rules/plans-and-adrs.md) (~25 lines) | Plans location (`.claude/plans/`, not `~/.claude/plans/`), ADR immutability, plans-vs-memory boundary | CLAUDE.md §Project State (plans + ADR paragraphs) + conventions.md tooling-shortcut plans-location |
| [`.claude/rules/code.md`](../rules/code.md) (~35 lines) | Rust / Go / TypeScript conventions + wire-protocol framing | CLAUDE.md §Rust + §Go + §TypeScript + §Wire Protocol |
| [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md) (~30 lines) | Numbered-list editing protocol, no silent SKIP, zero-manual-install, audit/refactor scope rule (no operational scripts), `/docs` is canonical | conventions.md §Editing protocol + §Scope rules |
| [`.claude/rules/tooling.md`](../rules/tooling.md) (~40 lines) | `make e2e` rule, full `make` target list, past-lesson `$HOME/go` trap | CLAUDE.md §Commands + conventions.md tooling-shortcuts (make e2e) + past-lessons (govulncheck) |

Rule files share a consistent header format:

```markdown
# <Rule Name>

**Enforced by:** `.claude/hooks/<hook-name>.sh` (if applicable) | **No bypass.**

<one-paragraph statement of the rule>

## Why

<one-paragraph rationale — usually a past-incident reference>

## How to apply

<bulleted edge cases or worked examples>
```

### 2. Rewrite `CLAUDE.md` as a one-page index

Reduces CLAUDE.md from 122 → ~40 lines. Structure:

```markdown
# OpenGate — Project Rules Index

CLAUDE.md is a one-page index. Each rule lives in its own focused file under
[`.claude/rules/`](.claude/rules/). MANDATORY rules are enforced by
[`.claude/hooks/`](.claude/hooks/) — no bypass mechanism exists.

## Project State — Read Before Starting Work

- [.claude/phases.md](.claude/phases.md) — completed / in-progress / planned
- [.claude/techdebt.md](.claude/techdebt.md) — tech debt by severity
- [.claude/decisions.md](.claude/decisions.md) — ADR index (full ADRs in [docs/adr/](docs/adr/))

Canonical developer docs live in [docs/](docs/). Start at [docs/Home.md](docs/Home.md);
read [docs/README.md](docs/README.md) before editing any doc.

## Workflow Rules

| Rule file | Concern | Enforced by |
|---|---|---|
| [git.md](.claude/rules/git.md) | branching, identity, commits, push | `git-commit-guard`, `git-push-guard` |
| [tdd.md](.claude/rules/tdd.md) | write failing test before source code | `tdd-gate`, `bash-source-write-guard` |
| [precommit-refactor.md](.claude/rules/precommit-refactor.md) | /precommit before commit; /refactor before push | marker files via commit/push guards |
| [sonarcloud.md](.claude/rules/sonarcloud.md) | quality-gate workflow; no suppressions | `write-guard` |
| [plans-and-adrs.md](.claude/rules/plans-and-adrs.md) | plans location, ADR immutability | `write-guard` |

## Code & Process Conventions

- [code.md](.claude/rules/code.md) — Rust / Go / TypeScript + wire protocol
- [editing-and-scope.md](.claude/rules/editing-and-scope.md) — editing protocol, scope rules
- [tooling.md](.claude/rules/tooling.md) — `make` targets, tooling shortcuts, past lessons
```

### 3. Delete `.claude/conventions.md`

Once content is distributed into the 8 rule files, [`conventions.md`](../conventions.md) is empty of unique content. Delete it. Update any inbound references.

### 4. Symlink `AGENT.MD` → `CLAUDE.md`

- Delete the current standalone `AGENT.MD` file (122 lines, untracked, byte-identical to CLAUDE.md).
- Create a symbolic link: `ln -s CLAUDE.md AGENT.MD`.
- `git add AGENT.MD` so the symlink (mode `120000`) is tracked. Once committed, the file cannot drift from CLAUDE.md — any tool that probes for `AGENT.MD` resolves to the same content.
- Note in `.claude/rules/plans-and-adrs.md` or `editing-and-scope.md` that `AGENT.MD` is a symlink — do not edit it as a separate file.

### 5. Delete duplicate feedback memories

All 8 feedback files in `/home/ivan/.claude/projects/-home-ivan-opengate/memory/` have been promoted into `conventions.md` (which dissolves in step 3 into the rule files). Delete:

- `feedback_use_make_e2e.md` → covered by [`.claude/rules/tooling.md`](../rules/tooling.md)
- `feedback_plans_location.md` → covered by [`.claude/rules/plans-and-adrs.md`](../rules/plans-and-adrs.md)
- `feedback_no_silent_skip.md` → covered by [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md)
- `feedback_zero_manual_install.md` → covered by [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md)
- `feedback_precommit_per_pr.md` → covered by [`.claude/rules/precommit-refactor.md`](../rules/precommit-refactor.md)
- `feedback_wiki_updates.md` → covered by [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md) (`/docs` canonical clause)
- `feedback_numbered_lists.md` → covered by [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md)
- `feedback_no_backup_script.md` → covered by [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md)

Update [`MEMORY.md`](/home/ivan/.claude/projects/-home-ivan-opengate/memory/MEMORY.md): remove the `## Feedback` section entirely. Add a one-line note pointing to [`.claude/rules/`](../rules/) for committed project conventions.

What stays in memory: `MEMORY.md` itself (environment, git config, design patterns, key interfaces — content not in the repo), `reference_meshcentral.md`, and any project context / research artifacts already there.

### 6. Update hooks to point at new rule files

Hook error messages currently reference CLAUDE.md sections or `.claude/conventions.md`. Update each hook's error/pointer text to cite the relevant rule file in [`.claude/rules/`](../rules/):

- [`.claude/hooks/session-start-context-load.sh`](../hooks/session-start-context-load.sh):
  - Line 37 (`Use 'make e2e', not bare 'npx playwright test' (.claude/conventions.md)`) → `(.claude/rules/tooling.md)`
  - Line 39 (`Conventions / lessons: see /home/ivan/opengate/.claude/conventions.md.`) → `Rules: see /home/ivan/opengate/.claude/rules/ (index in CLAUDE.md).`
- [`.claude/hooks/pretooluse-tdd-gate.sh`](../hooks/pretooluse-tdd-gate.sh) — repoint any "see CLAUDE.md §TDD Mandate" → "see .claude/rules/tdd.md"
- [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh) — repoint to `.claude/rules/git.md`, `.claude/rules/precommit-refactor.md`, `.claude/rules/tdd.md` per error context
- [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh) — repoint to `.claude/rules/git.md`, `.claude/rules/precommit-refactor.md`
- [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) — repoint to `.claude/rules/plans-and-adrs.md`, `.claude/rules/sonarcloud.md`
- [`.claude/hooks/pretooluse-bash-source-write-guard.sh`](../hooks/pretooluse-bash-source-write-guard.sh) — repoint to `.claude/rules/tdd.md`

The hardcoded TL;DR in `session-start-context-load.sh` lines 27–40 remains valuable (it's the at-a-glance list shown to the agent at session start). Update each line to cite the corresponding `.claude/rules/<name>.md` rather than the implicit CLAUDE.md section.

### 7. Update inbound references in skill SKILL.md files

Some skill `SKILL.md` files reference CLAUDE.md sections or specific phrases (e.g., precommit/SKILL.md references the marker rule). Grep for `CLAUDE.md` and `conventions.md` across `.claude/skills/*/SKILL.md` and update to the new paths.

## Critical Files

**New:**
- [`.claude/rules/git.md`](../rules/git.md)
- [`.claude/rules/tdd.md`](../rules/tdd.md)
- [`.claude/rules/precommit-refactor.md`](../rules/precommit-refactor.md)
- [`.claude/rules/sonarcloud.md`](../rules/sonarcloud.md)
- [`.claude/rules/plans-and-adrs.md`](../rules/plans-and-adrs.md)
- [`.claude/rules/code.md`](../rules/code.md)
- [`.claude/rules/editing-and-scope.md`](../rules/editing-and-scope.md)
- [`.claude/rules/tooling.md`](../rules/tooling.md)

**Modified:**
- [`CLAUDE.md`](../../CLAUDE.md) — rewritten as ~40-line index
- [`AGENTS.md`](../../AGENTS.md) — symlink → `CLAUDE.md` (tracked in git)
- [`.claude/hooks/session-start-context-load.sh`](../hooks/session-start-context-load.sh) — repoint conventions.md references to `.claude/rules/`
- [`.claude/hooks/pretooluse-tdd-gate.sh`](../hooks/pretooluse-tdd-gate.sh) — error-message repoint
- [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh) — error-message repoint
- [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh) — error-message repoint
- [`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh) — error-message repoint
- [`.claude/hooks/pretooluse-bash-source-write-guard.sh`](../hooks/pretooluse-bash-source-write-guard.sh) — error-message repoint
- [`.claude/skills/precommit/SKILL.md`](../skills/precommit/SKILL.md) and any other SKILL.md that references CLAUDE.md or conventions.md — repoint
- `/home/ivan/.claude/projects/-home-ivan-opengate/memory/MEMORY.md` — remove the `## Feedback` section; add a one-line pointer to `.claude/rules/`

**Deleted:**
- [`.claude/conventions.md`](../conventions.md) — content distributed into 8 rule files
- 8 feedback memory files under `/home/ivan/.claude/projects/-home-ivan-opengate/memory/feedback_*.md`

**Patterns to reuse:**
- The existing [`session-start-context-load.sh`](../hooks/session-start-context-load.sh) structure (TL;DR block + phases/techdebt extraction) — no architectural change, only text updates.
- The hook test runner at [`scripts/tests/hooks.test.sh`](../../scripts/tests/hooks.test.sh) (48 tests) — run after hook updates to confirm no behavior regression. Update any test that asserts on a specific error-message substring.
- Existing skill file headers (frontmatter + Description) — rule files don't need YAML frontmatter (they aren't discoverable units), just consistent Markdown headings.

## Verification

1. **CLAUDE.md is a one-page index.** `wc -l CLAUDE.md` returns ≤50.
2. **No duplicated rule content.** For each rule (TDD, branching, identity, precommit, refactor, SonarCloud, plans, ADRs, make-e2e, numbered-list-edit, zero-manual-install, no-operational-scripts, /docs canonical), `grep -c` in the new structure shows the rule statement appears in exactly one rule file under `.claude/rules/`. CLAUDE.md mentions the rule only as a one-line index row.
3. **AGENT.MD is a symlink.** `readlink AGENT.MD` returns `CLAUDE.md`. `git ls-files --stage AGENT.MD` shows mode `120000` (symlink).
4. **`conventions.md` is gone.** `test ! -e .claude/conventions.md` succeeds.
5. **Memory cleanup.** `ls /home/ivan/.claude/projects/-home-ivan-opengate/memory/feedback_*.md 2>/dev/null | wc -l` returns 0. `MEMORY.md` has no `## Feedback` section.
6. **No stale pointers.** `grep -rn "conventions\.md" .claude/ docs/ CLAUDE.md` returns no results.
7. **Hooks still pass their own tests.** Run [`scripts/tests/hooks.test.sh`](../../scripts/tests/hooks.test.sh) — all 48 hook tests pass. If any test asserts on a specific error-message substring that changed, update the assertion in the same PR.
8. **Session-start context-load works end-to-end.** Start a fresh Claude Code session in the repo. The TL;DR injected at session start cites `.claude/rules/<name>.md` for each rule, and the pointer at the end says "Rules: see /home/ivan/opengate/.claude/rules/".
9. **TDD gate exercise.** On a fresh branch off `dev`, attempt to edit a Go source file before any test change. The hook blocks with an error message that cites `.claude/rules/tdd.md`.
10. **Precommit/refactor gate exercise.** Stage a source-file change without running `/precommit`. Attempt `git commit`. The hook blocks with an error message that cites `.claude/rules/precommit-refactor.md`.
11. **`/precommit` and `/refactor` still pass.** `/precommit` writes the marker; `/refactor` writes its marker; the push-guard accepts a subsequent `git push origin dev`.
12. **Update [`phases.md`](../phases.md)** with a new Completed row: `Markdown Guideline Modularization | CLAUDE.md → 8-file index; conventions.md dissolved; AGENT.MD symlinked; 8 feedback memories pruned | — | [modularize-md-guidelines.md](plans/archive/modularize-md-guidelines.md)`. Move this plan file into `.claude/plans/archive/` after merge.

## Done-When

- CLAUDE.md is ≤50 lines and contains no rule statements — only index links.
- 8 rule files exist under `.claude/rules/`, each owning exactly one concern and ≤55 lines.
- `.claude/conventions.md` is deleted.
- `AGENT.MD` is a tracked symlink to `CLAUDE.md`.
- The 8 duplicated feedback memory files are deleted; `MEMORY.md` no longer references them.
- All 6 hooks point at the new rule files in their error messages and (where applicable) in the session-start TL;DR.
- `grep -rn "conventions\.md" .claude/ docs/ CLAUDE.md` returns no results.
- [`scripts/tests/hooks.test.sh`](../../scripts/tests/hooks.test.sh) passes (any asserted error-message substrings updated in the same PR).
- `/precommit` passes; the PR commits with the marker; `/refactor` passes; the push is accepted.
- `phases.md` updated with the Completed row; this plan archived.
