#!/usr/bin/env bash
# pretooluse-git-push-guard.sh — block bad pushes.
#
# Triggers on PreToolUse Bash; noop unless the command is `git push`.
# Enforces:
#   1. No push to main.
#   2. No force-push to main (any form).
#   3. Not behind upstream (best-effort; offline → skip).
#   4. /refactor marker matches HEAD for ANY commits since origin/dev,
#      regardless of the files they touch. A push is a push — there is no
#      doc-only / CI-only exemption.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

# Filter: command must include a `git push` verb.
if ! printf '%s' "$cmd" | grep -qE '\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+)*push\b'; then
  exit 0
fi

# 1 & 2. Target = main?
# Patterns that indicate main as the destination ref or refspec.
targets_main=false
if printf '%s' "$cmd" | grep -qE '\bgit[[:space:]].*push\b[^|;&]*\bmain\b'; then
  targets_main=true
fi

if [ "$targets_main" = "true" ]; then
  if printf '%s' "$cmd" | grep -qE -- '--force(-with-lease)?\b|[[:space:]]-f\b'; then
    block git-push-no-force-main "git push refused: force-push to main is never allowed. Remove --force / -f."
  fi
  block git-push-no-main "git push refused: target is main. .claude/rules/git.md: main updates only via the auto-merge CI job. Push to dev instead."
fi

# 3. Behind upstream (best-effort).
if git fetch --quiet origin dev 2>/dev/null; then
  if git rev-parse --verify --quiet origin/dev >/dev/null 2>&1; then
    behind="$(git rev-list --count HEAD..origin/dev 2>/dev/null || echo 0)"
    if [ "${behind:-0}" -gt 0 ] 2>/dev/null; then
      summary="$(git log --format='%h %s' HEAD..origin/dev 2>/dev/null | head -5 || true)"
      block git-push-behind "git push refused: local HEAD is $behind commit(s) behind origin/dev. Run: git pull --rebase origin dev. Upstream commits:
$summary"
    fi
  fi
fi

# 4. Refactor marker — required for ANY commit on the branch since origin/dev,
#    no matter what files it touches (no doc-only / CI-only exemption). The
#    auto-push hook and /refactor both write the marker = HEAD.
base=""
if git rev-parse --verify --quiet origin/dev >/dev/null 2>&1; then
  base="$(git merge-base HEAD origin/dev 2>/dev/null || true)"
fi
if [ -z "$base" ] && git rev-parse --verify --quiet dev >/dev/null 2>&1; then
  base="$(git merge-base HEAD dev 2>/dev/null || true)"
fi
if [ -z "$base" ] && git rev-parse --verify --quiet origin/main >/dev/null 2>&1; then
  base="$(git merge-base HEAD origin/main 2>/dev/null || true)"
fi
if [ -z "$base" ]; then
  base="$(git rev-list --max-parents=0 HEAD 2>/dev/null | head -1 || echo "")"
fi

if [ -n "$base" ] && [ "$base" != "$(git rev-parse HEAD 2>/dev/null || echo .)" ]; then
  marker_file="$(project_root)/.claude/.markers/refactor.head"
  head_sha="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
  if [ ! -f "$marker_file" ]; then
    block git-refactor-marker "git push refused: branch has commits since origin/dev but .claude/.markers/refactor.head is missing. Run /refactor; it writes the marker on success."
  fi
  expected="$(cat "$marker_file" 2>/dev/null || echo "")"
  if [ "$expected" != "$head_sha" ]; then
    block git-refactor-marker "git push refused: refactor marker ($expected) does not match HEAD ($head_sha). Re-run /refactor after the latest commit."
  fi
fi

exit 0
