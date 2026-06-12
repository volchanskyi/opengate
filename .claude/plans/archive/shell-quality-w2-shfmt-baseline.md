# W2 — Isolated shfmt Baseline

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 2
of 7. **Lands immediately after W1 on `dev`.**

## Goal

Reformat every tracked `.sh` with the pinned shfmt `3.13.1` and the W1 config, as
**one formatting-only commit** with zero functional or documentation changes.
shfmt's dry run reported ~1,370 changed lines across ~42 files — mechanical
layout only.

## Why isolated

- A formatting reformat mixed with logic changes makes review impossible and
  hides regressions. The commit must be `shfmt`-only.
- It must land before W4 wires the shfmt **diff** check into the gauntlet, and
  before the six `fast-path-*` branches rebase, so they rebase over one stable,
  formatting-only target (Guardrail A).

## Preconditions

- W1 is merged on `dev` (provisioner, config, runner, and — critically — at
  least one `*.test.sh` change, which keeps the TDD/source-write hooks silent
  for this source-only reformat).
- Pending documentation-doctrine hook edits land first so this reformat does not collide
  with pending hook work.

## File inventory

- **Modified:** every tracked `.sh` that shfmt rewrites (~42 files across
  `scripts/`, `scripts/tests/`, `.claude/hooks/`, `deploy/scripts/`,
  `server/internal/api/install.sh`). No new files. No non-`.sh` files.

## Steps

1. Confirm the pinned shfmt is provisioned: `scripts/install-shell-tools.sh`.
2. Capture a pre-image: run the **full shell test suite** (`make shell-test`)
   and record green.
3. `scripts/shell-quality.sh format` (writes shfmt over `git ls-files '*.sh'`).
4. Run `make shell-test` again — **must be identically green**. Any test
   behavior change means shfmt altered semantics (heredoc, trap, or nested-shell
   string); investigate before proceeding.
5. Manually diff-review the high-risk scripts to confirm semantics are
   unchanged: heredocs (`<<-`/`<<`), `trap` lines, and nested-shell string
   literals in [`precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh),
   [`bastion-session.sh`](../../../deploy/scripts/bastion-session.sh), and
   [`install.sh`](../../../server/internal/api/install.sh).
6. `git diff --stat` — confirm only `.sh` files changed.
7. `/precommit` → commit (message states "formatting only, shfmt 3.13.1 baseline,
   no behavior change") → `/refactor` → push.

## Out of scope

- Any logic, comment-wording, strict-mode, or policy change (those are W3+).
- Enforcing the shfmt diff in the gauntlet (**W4**).
- Workflow/composite-action YAML shell fragments (excluded by the master plan —
  GitHub expressions aren't standalone Bash).

## Reviewer checklist

- [ ] Diff touches **only** `.sh` files; zero `.md`/`.yml`/source-logic changes.
- [ ] `git log -1 --stat` shows a formatting-only commit; the message says so.
- [ ] `make shell-test` is green both before and after (attach evidence).
- [ ] Spot-check heredocs/traps/nested-shell strings in the three high-risk
      scripts — no semantic drift.
- [ ] `scripts/shell-quality.sh format` produces **no further diff** when re-run
      (idempotent format).
- [ ] No deprecated shfmt flags (`-kp`/`keep_padding`) appear in config or
      invocation.
