#!/usr/bin/env bash
# session-start-context-load.sh — inject project rules + phase status.
#
# Runs alongside session-start-fetch.sh. Outputs additionalContext JSON
# that surfaces:
#   - TL;DR of mandatory rules + which hook enforces each
#   - In Progress + Planned + last 10 Completed rows from .claude/phases.md
#   - Critical / High items from .claude/techdebt.md
#   - Summary of any prior-session blocks (last 20 entries)
#   - Pointer to .claude/rules/ (rules index in CLAUDE.md)
#
# Always exit 0; SessionStart hooks must never block.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

# Discard stdin — we don't need any field from it.
cat >/dev/null 2>&1 || true

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
phases="$repo/.claude/phases.md"
techdebt="$repo/.claude/techdebt.md"
session_id="${CLAUDE_SESSION_ID:-unknown}"
uid="${UID:-$(id -u 2>/dev/null || echo 0)}"
blocks_log="${TMPDIR:-/tmp}/claude-${uid}/${session_id}/blocks.log"

read -r -d '' tldr <<'EOF' || true
MANDATORY RULES (enforced by .claude/hooks/, NO bypass):
  - Work on dev only; never commit/push to main (git-commit-guard, git-push-guard)
  - Author = Ivan Volchanskyi <ivan.volchanskyi@gmail.com>; no Co-Authored-By (git-commit-guard)
  - TDD: write failing test BEFORE source code (tdd-gate, bash-source-write-guard, git-commit-guard backup)
  - Run /precommit before every commit; marker validates via git write-tree (git-commit-guard)
  - Run /refactor after /precommit; marker validates via git rev-parse HEAD (git-push-guard)
  - Plans live in /home/ivan/opengate/.claude/plans/, NOT ~/.claude/plans/ (write-guard)
  - ADRs in docs/adr/ are immutable — supersede with new file (write-guard)
  - No NOSONAR / //nolint / sonar.issue.ignore / eslint-disable (write-guard)
  - Use `make e2e`, not bare `npx playwright test` (.claude/rules/tooling.md)

Rules: see /home/ivan/opengate/.claude/rules/ (index in CLAUDE.md).
EOF

phases_section=""
if [ -f "$phases" ]; then
  in_progress=$(awk '/^## In Progress/{flag=1; next} /^## /{flag=0} flag' "$phases" 2>/dev/null | head -20 || true)
  planned=$(awk '/^## Planned/{flag=1; next} /^## /{flag=0} flag' "$phases" 2>/dev/null | head -30 || true)
  last_completed=$(grep -E '^\| (Phase |Structural |Claude |CD Phase |Broad |Test |File |Display |X11 |System |CI |Linux |Agent |SonarCloud |Dev |Device |Coverage |Phase )' "$phases" 2>/dev/null | tail -10 || true)

  phases_section="$(printf '\n--- phases.md ---\n## In Progress\n%s\n\n## Planned\n%s\n\n## Most recent 10 Completed rows:\n%s\n' "$in_progress" "$planned" "$last_completed")"
fi

techdebt_section=""
if [ -f "$techdebt" ]; then
  crithigh=$(awk '/^## (Critical|High)/,/^## /' "$techdebt" 2>/dev/null | head -60 || true)
  techdebt_section="$(printf '\n--- techdebt.md (Critical + High) ---\n%s\n' "$crithigh")"
fi

prior_blocks_section=""
if [ -d "${TMPDIR:-/tmp}/claude-${uid}" ] && [ ! -f "$blocks_log" ]; then
  # Walk the last few session dirs, collecting their blocks.log entries.
  last_blocks="$(find "${TMPDIR:-/tmp}/claude-${uid}" -maxdepth 2 -name blocks.log -mtime -7 -print0 2>/dev/null \
    | xargs -0 -r cat 2>/dev/null | tail -20 || true)"
  if [ -n "$last_blocks" ]; then
    prior_blocks_section="$(printf '\n--- Recent prior-session blocks (informational) ---\n%s\n' "$last_blocks")"
  fi
fi

full="${tldr}${phases_section}${techdebt_section}${prior_blocks_section}"

# Emit JSON.
MSG="$full" python3 -c '
import json, os, sys
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "SessionStart",
        "additionalContext": os.environ["MSG"],
    }
}))
'
