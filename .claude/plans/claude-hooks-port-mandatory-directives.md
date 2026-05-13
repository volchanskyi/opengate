# Port CLAUDE.md mandatory directives to deterministic Claude Code hooks

## Context

The coding agent repeatedly forgets project conventions written as prose in [CLAUDE.md](../../CLAUDE.md) and in per-user memory at [~/.claude/projects/-home-ivan-opengate/memory/](../../../.claude/projects/-home-ivan-opengate/memory/). Examples: not running `/precommit` before commit, missing `/refactor` after a commit, skipping local SonarCloud scans, **starting to code without a failing test** (violating the TDD mandate), accepting `Co-Authored-By` trailers, writing plans to `~/.claude/plans/` instead of the project dir. The rules are unambiguous and shell-checkable, but the harness has no enforcement layer beyond CLAUDE.md text.

This plan moves the enforceable directives into [.claude/hooks/](../hooks/) scripts that the Claude Code harness runs deterministically around tool calls. Hooks **cannot directly invoke skills** like `/precommit`; the bridge is a **marker file** that the skill writes on success and the hook reads. Two additional concerns are folded in: (1) promote project-critical memory files into a committed conventions file so fresh agents / other machines see them; (2) **enforce every rule 100% of the time — no bypass mechanism of any kind exists**.

**Core enforcement principle.** Every rule in this plan is a hard block. There is no `OPENGATE_HOOK_BYPASS` env var. There is no master kill switch. There is no commit-message escape phrase. There is no marker file you can pre-create to skip a check. The only way to change enforcement is to edit `.claude/settings.json` (which is itself a deliberate, code-reviewed change). This is by user directive: "MAKE SURE ALL THESE rules are followed 100% of the time."

**TDD enforcement design principle.** The test-first rule fires *before* coding begins, not at commit time. The hook blocks `Write/Edit/MultiEdit` on source files when the current branch has no test changes yet. Catching the violation at `git commit` would be after the fact — by then the agent has already written code first. The block fires on the *first* source edit per branch; once a test exists on the branch, subsequent source edits flow normally.

**Out of scope (per user direction):** the `AGENT.MD` / `AGENTS.md` / `CLAUDE.md` filename migration is **not** part of this change. CLAUDE.md remains the canonical source. The untracked `AGENT.MD` symlink is left as it is. CI/GitHub Actions changes are **also out of scope** — enforcement lives entirely in local hooks for this change.

User decisions (locked in): hard-block via marker files for `/precommit` and `/refactor`; promote project-relevant memory into a committed file; scope covers git safety, skill gating, plan/ADR/Sonar guardrails, session context injection, TDD enforced at the point of first source edit; **all rules without exception, no bypass**.

---

## 1. File inventory

### Created
| Path | Purpose |
|---|---|
| `.claude/hooks/lib/common.sh` | Shared helpers: JSON output, marker paths, source-vs-test classifier, blocked-operation logging |
| `.claude/hooks/lib/branch-tdd.sh` | Shared "does this branch have a test change yet?" check, sourced by hooks |
| `.claude/hooks/session-start-context-load.sh` | Inject TL;DR rules, In-Progress / Planned / last-10-Completed rows from `phases.md`, top techdebt, plan dir reminder, prior-session block summary |
| `.claude/hooks/pretooluse-tdd-gate.sh` | Block `Write/Edit/MultiEdit` of source files when no test exists on the branch (primary TDD enforcement) |
| `.claude/hooks/pretooluse-git-commit-guard.sh` | Block bad commits (identity, --no-verify, Co-Authored-By, branch, behind upstream, precommit marker, TDD backup check) |
| `.claude/hooks/pretooluse-git-push-guard.sh` | Block bad pushes (to main, refactor marker missing, behind upstream) |
| `.claude/hooks/pretooluse-write-guard.sh` | Block writes/edits to forbidden paths/content (plans dir, ADR, NOSONAR/nolint/sonar.issue.ignore) |
| `.claude/hooks/pretooluse-bash-source-write-guard.sh` | Catch source-file modification via Bash (`>`, `>>`, `sed -i`, `tee`) and apply TDD gate |
| `.claude/conventions.md` | Project-relevant conventions promoted from per-user memory |
| `scripts/tdd-check.sh` | Shared TDD classifier sourced by the hooks (single source of truth) |
| `scripts/tests/tdd-check.bats` | Unit tests for the classifier; required so PR 2 satisfies its own TDD gate |
| `.claude/.markers/.gitkeep` | Keep the markers dir in the working tree |

### Modified
| Path | Change |
|---|---|
| [.claude/settings.json](../settings.json) | Add hook stanzas (tdd-gate, write-guard, bash-source-write-guard, commit-guard, push-guard) plus new SessionStart entry. Keep existing fetch hook. |
| [.claude/skills/precommit/SKILL.md](../skills/precommit/SKILL.md) | Append "Marker file (mandatory final step)" — `git write-tree > .claude/.markers/precommit.head` after all gates pass |
| [.claude/skills/refactor/SKILL.md](../skills/refactor/SKILL.md) | Append analogous marker section — `git rev-parse HEAD > .claude/.markers/refactor.head` |
| [CLAUDE.md](../../CLAUDE.md) | Append one short sentence under each MANDATORY block: "Enforced by `.claude/hooks/<name>.sh`, no bypass." No filename migration. |
| `.gitignore` | Ignore `.claude/.markers/*` except `.gitkeep` |

### Unchanged (explicitly out of scope)
- `AGENT.MD` (untracked symlink) — left as-is.
- `AGENTS.md` — not created.
- The `CLAUDE.md` filename / symlink relationship — left as-is.
- `.github/workflows/*` — no CI mirror; enforcement is local-only.

---

## 2. Hook-by-hook spec

All scripts in [.claude/hooks/](../hooks/), `chmod +x`, source `lib/common.sh`. **No script honors any bypass environment variable.** No script consults a kill switch.

### 2.1 `session-start-context-load.sh`
- **Event:** SessionStart (runs alongside existing `session-start-fetch.sh`).
- **Budget:** <500ms; file reads only.
- **Output (additionalContext JSON):**
  - TL;DR mandatory rules (8 bullets), each annotated with "enforced by hook X, no bypass".
  - From `.claude/phases.md`:
    - **In Progress** section, full content.
    - **Planned** section, full content.
    - Last 10 rows of the **Completed** table (`awk '/^\| Phase /{ ... }' | tail -10`) — surfaces what shipped recently.
  - From `.claude/techdebt.md`: Critical and High items (`awk '/^## (Critical|High)/,/^## /' | head -60`).
  - Summary of any prior-session blocks (last 20 entries from the blocked-operation log) — visibility into what the agent tried and was prevented from doing.
  - Reminder: plans go in `/home/ivan/opengate/.claude/plans/` and CLAUDE.md remains canonical.
- Always exit 0.

### 2.2 `pretooluse-tdd-gate.sh` — primary TDD enforcement

- **Event:** PreToolUse, **matcher** `Write|Edit|MultiEdit`.
- **Budget:** <300ms (one `git diff --name-only` + classifier).
- **Logic:**

  ```
  path = $CLAUDE_TOOL_INPUT_file_path
  if path is NOT source (test/doc/config/generated) → exit 0
  if branch has any test change (scripts/tdd-check.sh) → exit 0
  → BLOCK exit 2
  ```

  **Source classification** (matches AND not in exclusion list):
  - matches: `\.(go|rs|tsx?|jsx?)$`
  - excludes (treated as non-source): `_test\.go|_test\.rs|\.test\.(ts|tsx|js|jsx)|_spec\.(ts|tsx|js|jsx)|/tests/|/test/|/__tests__/|openapi_gen\.go|.*_gen\.go|.*\.pb\.go`
  - Generated files excluded so codegen output doesn't trip the gate.

  **"Branch has test change" check** (`scripts/tdd-check.sh`):
  1. `base = git merge-base HEAD origin/dev` (fallback `origin/main`, fallback initial commit).
  2. Union of:
     - committed: `git diff --name-only "$base"..HEAD -- <test-globs>`
     - staged: `git diff --cached --name-only -- <test-globs>`
     - unstaged tracked: `git diff --name-only -- <test-globs>`
     - untracked: `git ls-files --others --exclude-standard -- <test-globs>`
  3. Success if ≥1 file.

  `<test-globs>`: `'*_test.go' '*_test.rs' '*.test.ts' '*.test.tsx' '*.test.js' '*.test.jsx' '*_spec.ts' '*_spec.tsx' 'tests/' 'test/' '__tests__/' 'web/e2e/'`

- **Block message:**
  ```
  TDD violation. Per CLAUDE.md §TDD Mandate, the failing test MUST be
  written BEFORE the source code.

  This branch (since {base}) has no test files modified, added, or staged.
  Before editing {path}, do ONE of:
    - add a new test file under server/internal/<pkg>/*_test.go,
      agent/src/.../tests/, or web/src/**/__tests__/
    - extend an existing test file with a NEW failing assertion that
      covers the change you are about to make

  Once a test exists on this branch, this hook will not fire again on
  this branch — subsequent source edits flow normally.

  There is NO bypass. The rule is enforced 100% of the time.
  ```

- **TDD lifecycle correctness:**
  - **New feature, fresh branch:** edit `*_test.go` first (test edits not gated). Then `*.go` allowed.
  - **Pure refactor:** touch the covering test first (strengthen an assertion, or add a `// covers …` comment + assertion). Matches CLAUDE.md TDD step 3.
  - **Bug fix:** write a failing regression test, then fix the source.
  - **New source file:** `Write` is gated the same as `Edit`; test must exist first.

- **Acknowledged false-positive:** typo-only fix in a source comment fires the gate. Resolution: also touch the relevant test (no bypass — by design).

### 2.3 `pretooluse-bash-source-write-guard.sh` — catch shell-based source writes

- **Event:** PreToolUse, **matcher** `Bash`.
- **Purpose:** §2.2 catches Claude-tool-based source edits. Shell can also write files. This hook scans Bash commands for shell-write patterns targeting source paths and applies the same TDD gate.
- **Budget:** <150ms.
- **Logic:**
  ```
  cmd = $CLAUDE_TOOL_INPUT_command
  candidates = paths extracted from cmd matching:
      - redirection: '>[[:space:]]*([^[:space:]&|;]+)', '>>[[:space:]]*(…)'
      - sed in-place: 'sed[[:space:]]+(-[A-Za-z]*i[A-Za-z]*[^[:space:]]*[[:space:]]+)+([^[:space:]]+)'
      - tee target:   'tee[[:space:]]+(-[a-z]+[[:space:]]+)?([^[:space:]]+)'
  for path in candidates:
      resolve to absolute path within repo working tree
      if path is source AND branch has no test change:
          BLOCK exit 2 with §2.2 block message, citing the bash form
  ```
- Best-effort regex; the commit-guard TDD backup check (§2.4 rule 7) is the final safety net.

### 2.4 `pretooluse-git-commit-guard.sh`
- **Event:** PreToolUse, **matcher** `Bash`.
- **Filter:** exit 0 unless command matches `\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+)*commit\b`.
- **Budget:** <1500ms.
- **Rules (all block with exit 2; no bypass):**
  1. **No `Co-Authored-By`:** block if command contains the string (case-insensitive). Fix: remove the trailer.
  2. **No `--no-verify`:** block if present. Fix: don't bypass hooks.
  3. **Identity match:** parse `-c user.email=` / `-c user.name=` flags; fall back to `git config`. Block if identity ≠ `Ivan Volchanskyi <ivan.volchanskyi@gmail.com>`. Fix: `git config user.name "Ivan Volchanskyi" && git config user.email "ivan.volchanskyi@gmail.com"`.
  4. **Branch not `main`:** block if HEAD is `main`. Block for any non-`dev` branch except recognised PR prefixes (`feat/*`, `fix/*`, `chore/*`, `refactor/*`, `docs/*`, `test/*`, `ci/*`).
  5. **Not behind upstream:** `git fetch --quiet origin dev` (4s timeout); block if `rev-list --count HEAD..origin/dev > 0`. Fix: `git pull --rebase origin dev`.
  6. **Precommit marker valid:** compare `git write-tree` to `.claude/.markers/precommit.head`. Block on mismatch or missing. Fix: re-run `/precommit`. (Marker uses `write-tree`, not `rev-parse HEAD`, because `/precommit` validates staged content; the marker must invalidate when staging changes.)
  7. **TDD backup check:** invoke `scripts/tdd-check.sh`. Block if commit would land source changes on a branch with no test changes anywhere.

### 2.5 `pretooluse-git-push-guard.sh`
- **Event:** PreToolUse, **matcher** `Bash`.
- **Filter:** command matches `\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+)*push\b`.
- **Budget:** <2000ms.
- **Rules (all block, no bypass):**
  1. **No push to `main`:** block if target is `main` (literal `origin main`, `HEAD:main`, or `--force` with `main`). Fix: push to `dev`.
  2. **No force-push to `main`:** block if `--force`/`--force-with-lease` AND target is `main`. Fix: remove `--force`.
  3. **Not behind upstream:** `git fetch origin dev`; block if `rev-list --count HEAD..origin/dev > 0`. Fix: `git pull --rebase origin dev`.
  4. **Refactor marker valid:** for commits in `origin/dev..HEAD`, if any touched source paths (same classifier as §2.2), require `.claude/.markers/refactor.head == HEAD`. Doc-only / CI-only pushes exempted. Fix: run `/refactor`.

### 2.6 `pretooluse-write-guard.sh`
- **Event:** PreToolUse, **matcher** `Write|Edit|MultiEdit`.
- **Budget:** <200ms.
- **Rules (all block, no bypass):**
  1. **Plans dir:** block if `file_path` matches `^(/?home/ivan/|~/)\.claude/plans/`. Fix: write under `/home/ivan/opengate/.claude/plans/`.
  2. **ADR immutability:** block `Edit|MultiEdit` and `Write` *to existing files* matching `.*/docs/adr/ADR-\d+.*\.md$`. New ADR files allowed. Fix: supersede with a new ADR.
  3. **Sonar suppression:** scan `new_string` / `content` for `NOSONAR`, `//nolint`, `nolint:`, `sonar\.issue\.ignore\.multicriteria`, `eslint-disable*` → BLOCK. Fix: restructure code.

### 2.7 Marker and log file locations
- **Markers:** `.claude/.markers/{precommit,refactor}.head` (repo-local; gitignored).
- **Blocked-operation log:** `${TMPDIR:-/tmp}/claude-${UID}/${CLAUDE_SESSION_ID}/blocks.log` — every block appends `timestamp\trule\thook-script\tcommand_or_path`. Read by the next session's SessionStart hook for visibility. Informational only; does not allow bypass.

### 2.8 Behavior on internal hook failure — fail closed

Every hook uses `set -euo pipefail` with a `trap` that exits 2 on unexpected error. If a hook can't determine whether a rule is satisfied (git command fails, dependency missing, malformed input), it BLOCKS. A broken hook surfaces immediately as a block, not a silent allowance.

**Escape hatch for a genuinely broken hook:** edit `.claude/settings.json` to comment out the offending hook entry. That's a deliberate, version-controlled change, not a runtime bypass. (Editing `settings.json` is permitted by the write-guard — it's not in the blocked paths, and `.json` is not source for the TDD gate.)

---

## 3. Skill changes

### 3.1 `.claude/skills/precommit/SKILL.md`
Append after the existing "Gate Criteria" section:

> ## Marker file (mandatory final step)
>
> After all gates pass, run as the absolute last step before returning control:
>
>     mkdir -p .claude/.markers
>     git write-tree > .claude/.markers/precommit.head
>
> `pretooluse-git-commit-guard.sh` reads this and blocks `git commit` unless it equals `git write-tree` at commit time. Re-staging invalidates it — re-run `/precommit`. There is NO bypass.
>
> If ANY gate failed, do NOT write the marker.

Add a "TDD interaction" subsection: the TDD gate (`pretooluse-tdd-gate.sh`) fires at the first source-file edit on a branch, not at commit time. `/precommit` should sanity-check that the branch's test diff is non-empty when source diff is non-empty; alert the user if not (likely hook misconfiguration).

### 3.2 `.claude/skills/refactor/SKILL.md`
Append:

> ## Marker file (mandatory final step)
>
> After completing the refactor and confirming tests still pass:
>
>     mkdir -p .claude/.markers
>     git rev-parse HEAD > .claude/.markers/refactor.head
>
> `pretooluse-git-push-guard.sh` blocks `git push` when commits since `origin/dev` touch source files unless this marker equals HEAD. NO bypass.

### 3.3 `.gitignore`
Append:

```
# Hook marker files — local state only
.claude/.markers/*
!.claude/.markers/.gitkeep
```

### 3.4 `CLAUDE.md` text additions

Under each existing MANDATORY block, append a one-line sentence pointing at the hook that enforces it. Examples:

- After line 27 ("All work happens on `dev`. No exceptions."): "Enforced by `.claude/hooks/pretooluse-git-commit-guard.sh` and `pretooluse-git-push-guard.sh`. No bypass."
- After line 34 (identity rule): "Enforced by `.claude/hooks/pretooluse-git-commit-guard.sh`. No bypass."
- After line 39 (TDD): "Enforced by `.claude/hooks/pretooluse-tdd-gate.sh`. No bypass."
- After line 69 (`/precommit` mandate): "Enforced by `.claude/hooks/pretooluse-git-commit-guard.sh` via marker `.claude/.markers/precommit.head`. No bypass."
- After line 72 (`/refactor` mandate): "Enforced by `.claude/hooks/pretooluse-git-push-guard.sh` via marker `.claude/.markers/refactor.head`. No bypass."
- After line 95 (Sonar suppression rule): "Enforced by `.claude/hooks/pretooluse-write-guard.sh`. No bypass."

---

## 4. Conventions file: `.claude/conventions.md`

**Path choice:** alongside `phases.md` / `techdebt.md` / `decisions.md`. Keeps CLAUDE.md focused on rules; conventions.md captures lessons / shortcuts / worked examples.

**Sections:**
- Workflow rules (one-line summaries; point at CLAUDE.md for canonical text and the enforcing hook for each)
- Tooling shortcuts (`make e2e` not `npx playwright test`; `/precommit` after every PR; project-local plans dir)
- Editing protocol (read whole file before editing numbered lists; never claim "SKIP" passes; zero manual install steps for the agent)
- Scope rules (no operational scripts in plans unless asked; `/docs` updated each phase)
- **TDD lifecycle worked examples** — for each scenario, show the *exact sequence of tool calls* and which hooks fire:
  - New-feature flow: test first, then code.
  - Bug-fix flow: regression test first, then fix.
  - Pure-refactor flow: touch the covering test, then refactor.
  - Generated-file flow: codegen output excluded; agent uses Bash to run the generator.
- Past lessons (one-line summaries)

**Memory → conventions migration table:**

| Memory file | Decision | Rationale |
|---|---|---|
| `feedback_use_make_e2e.md` | **Promote** | Project tooling rule |
| `feedback_no_silent_skip.md` | **Promote** | Precommit-gate behaviour |
| `feedback_precommit_per_pr.md` | **Promote** | Workflow rule |
| `feedback_zero_manual_install.md` | **Promote** | Deployment policy |
| `feedback_wiki_updates.md` | **Promote (rephrase)** | Wiki deprecated; rephrase to "/docs updates per phase" |
| `feedback_plans_location.md` | **Promote** | Hook-enforced; reinforce in prose |
| `feedback_numbered_lists.md` | **Promote** | Editing protocol |
| `feedback_no_backup_script.md` | **Promote** | Plan-scope policy |
| `MEMORY.md` top-level facts | **Leave (personal)** | Per-machine env specifics |
| `phase-*.md`, `phaseN-plan.md`, `project_*.md`, `quic-stream-ownership-fix.md`, `reference_meshcentral.md`, `cira-protocol-reference.md` | **Leave** | Stale notes/research; not active rules |

---

## 5. `.claude/settings.json` changes

**Location:** committed `settings.json`. `settings.local.json` is gitignored / per-machine; hooks must fire for any human/agent working in this repo.

**Diff to `hooks` block:**
```jsonc
"hooks": {
  "SessionStart": [
    { "hooks": [
        { "type": "command", "command": ".claude/hooks/session-start-fetch.sh",        "timeout": 8 },
        { "type": "command", "command": ".claude/hooks/session-start-context-load.sh", "timeout": 4 }
    ]}
  ],
  "PreToolUse": [
    { "matcher": "Write|Edit|MultiEdit",
      "hooks": [
        { "type": "command", "command": ".claude/hooks/pretooluse-tdd-gate.sh",        "timeout": 3 },
        { "type": "command", "command": ".claude/hooks/pretooluse-write-guard.sh",     "timeout": 2 }
    ]},
    { "matcher": "Bash",
      "hooks": [
        { "type": "command", "command": ".claude/hooks/pretooluse-bash-source-write-guard.sh", "timeout": 2 },
        { "type": "command", "command": ".claude/hooks/pretooluse-git-commit-guard.sh",        "timeout": 4 },
        { "type": "command", "command": ".claude/hooks/pretooluse-git-push-guard.sh",          "timeout": 6 }
    ]}
  ]
}
```

**Permissions:** no new `permissions.allow` entries — hook scripts are invoked by the harness, not by Claude through the Bash tool.

---

## 6. Enforcement model — no bypass, no exceptions

There is **no runtime escape**. Each rule fires every time its conditions are met. There is no env var (`OPENGATE_HOOK_BYPASS`), no kill switch (`OPENGATE_HOOKS_ENABLED`), no commit-message phrase (`[skip-hook]`), and no marker file the agent can pre-create to silence a hook.

**The only way to change enforcement is to modify `.claude/settings.json`** — a tracked, reviewable, version-controlled file. That edit is visible in `git status` and any future PR diff. Turning off a rule is a deliberate code change, not a runtime trick.

**Blocked-operation log** (`${TMPDIR}/claude-${UID}/${CLAUDE_SESSION_ID}/blocks.log`) is informational only. Records what was attempted and blocked so the next session's SessionStart hook can surface it. Does not weaken any rule.

**Hook failure behavior: fail closed.** If a hook itself errors, it exits 2 (BLOCK) rather than 0 (ALLOW). A broken hook surfaces as a block, not a silent allowance. The user fixes the hook (Edit on a `.sh` file — not a source-language file, so the TDD gate doesn't fire). After committing the fix through normal channels, enforcement resumes.

---

## 7. Verification / acceptance criteria

Run on scratch branches off `dev`. Every test asserts BLOCK or PASS; no "bypass works" cases exist.

**TDD gate (primary, no bypass):**
- Fresh branch off `dev`. `Edit /home/ivan/opengate/server/internal/api/handlers.go` → BLOCKED.
- Fresh branch. `Edit /home/ivan/opengate/server/internal/api/handlers_test.go` first → ALLOWED. Then `Edit handlers.go` → ALLOWED.
- Fresh branch. `Write /home/ivan/opengate/server/internal/api/new_handler.go` → BLOCKED.
- Fresh branch. `Edit /home/ivan/opengate/server/internal/api/openapi_gen.go` → ALLOWED (generated, excluded).
- Fresh branch. `Edit /home/ivan/opengate/docs/Home.md` → ALLOWED (not source).
- `OPENGATE_HOOK_BYPASS=anything Edit handlers.go` → still BLOCKED (var ignored).

**Bash source-write guard:**
- Fresh branch. `Bash: echo "package main" > server/internal/foo/new.go` → BLOCKED.
- Fresh branch. `Bash: sed -i 's/old/new/' server/internal/api/handlers.go` → BLOCKED.
- Fresh branch. `Bash: echo "ok" > /tmp/scratch.txt` → ALLOWED (outside repo).
- After staging a test, both source-write cases ALLOWED.

**Commit-guard:**
- `git commit -m "feat: x"` with no marker → BLOCKED.
- `/precommit` writes marker → `git commit` → PASS.
- `git add otherfile.txt` between marker and commit → BLOCKED (write-tree changed).
- `Co-Authored-By:` in message → BLOCKED.
- `git -c user.email=other@example.com commit -m "x"` → BLOCKED on identity.
- `git commit --no-verify -m "x"` → BLOCKED.
- On `main` branch: any commit → BLOCKED.
- TDD backup: source-only branch diff → BLOCKED.

**Push-guard:**
- `git push origin main` → BLOCKED.
- `git push --force origin main` → BLOCKED.
- Behind upstream → BLOCKED.
- Source commits without refactor marker → BLOCKED.
- Doc-only commits without refactor marker → PASS.

**Write-guard:**
- Write to `~/.claude/plans/foo.md` → BLOCKED.
- Edit existing `docs/adr/ADR-013-*.md` → BLOCKED.
- Write new `docs/adr/ADR-099-new.md` → PASS.
- Edit adding `// NOSONAR` → BLOCKED.
- Edit adding `eslint-disable-next-line` → BLOCKED.

**SessionStart context-load:**
- Fresh session → TL;DR + In-Progress + Planned + last-10-Completed + critical/high techdebt injected.
- Prior session with blocks → next session shows "previous session blocks: …".

**False-positive risk register (acknowledged; no bypass provided):**

| Hook / rule | Risk | Resolution (no bypass) |
|---|---|---|
| commit-guard / identity | Typo in `-c user.email=` | Fix the command. Error message shows the offending value. |
| commit-guard / precommit-marker | Re-staging invalidates marker | Re-run `/precommit`. |
| **tdd-gate** | Typo-fix in a source comment is blocked | Adjust the relevant test alongside (strengthen an assertion or add a comment-comparison). |
| **bash-source-write-guard** | `>` to a path that looks like source but isn't | Regex requires path under repo working tree; paths outside ignored. |
| push-guard / refactor-marker | Path classifier flags doc commits | File-extension globs + explicit doc/CI allowlist. |
| write-guard / plans-wrong-dir | Personal scratch outside the project | Use a path outside `~/.claude/plans/`. |
| write-guard / sonar-suppress | Test fixture literal containing `NOSONAR` | Refactor the fixture to construct the string at runtime. |
| write-guard / adr-immutable | Editing `docs/adr/README.md` (index) | Regex `ADR-\d+.*\.md` excludes the index. |

---

## 8. Rollout sequence

Bootstrap requires care because the hooks block enforcement of their own rules. Sequence:

### PR 1: Foundation (no enforcement activated yet)
- `.claude/conventions.md` from memory promotions (§4).
- `.claude/.markers/.gitkeep` + `.gitignore` update.
- `scripts/tdd-check.sh` (shared classifier; used by hooks starting PR 2).
- `scripts/tests/tdd-check.bats` — unit test for the classifier. Required so PR 2 satisfies the TDD gate when it adds the hook scripts.
- Append "Enforced by `.claude/hooks/<name>.sh` (PR 2), no bypass" sentences to CLAUDE.md MANDATORY blocks.
- No hook scripts yet, no `settings.json` hook changes. Behavior unchanged.

### PR 2: Hooks + skill markers + settings.json (must ship together)

Hooks are loaded from `settings.json` at **session start**; changes during an active session do not retroactively apply. Therefore PR 2 must be authored in a single session that started before `settings.json` was updated.

- **Author order within the session:**
  1. Write all new hook scripts under `.claude/hooks/` (`.sh` files — not source for TDD gate).
  2. Extend `scripts/tdd-check.sh` if needed (already has a test from PR 1, so the TDD gate passes).
  3. Append marker sections to the two `SKILL.md` files (markdown — not source for TDD gate).
  4. Update `.claude/settings.json` last.
- **None of PR 2's files are source-language files** (`*.go|*.rs|*.tsx?|*.jsx?`), so the TDD gate wouldn't fire even if it were active mid-session.
- **Marker bootstrap:** at the end of the PR 2 session, run `/precommit` (which writes `.claude/.markers/precommit.head`), then `git commit`. The commit-guard isn't active yet in the current session, but the marker is in place.
- **First session after PR 2 merges** has all hooks active. Subsequent work fully governed.

---

## Critical files
- [.claude/settings.json](../settings.json)
- [.claude/skills/precommit/SKILL.md](../skills/precommit/SKILL.md)
- [.claude/skills/refactor/SKILL.md](../skills/refactor/SKILL.md)
- [.claude/hooks/session-start-fetch.sh](../hooks/session-start-fetch.sh) (untouched; new sibling scripts added)
- [CLAUDE.md](../../CLAUDE.md) (text additions only — no rename, no symlink change)
- [.gitignore](../../.gitignore)
- `.claude/conventions.md` (new)
- `scripts/tdd-check.sh` + `scripts/tests/tdd-check.bats` (new; shared classifier + test)
- `.claude/hooks/lib/common.sh`, `.claude/hooks/lib/branch-tdd.sh`, and 6 new hook scripts (all new)
