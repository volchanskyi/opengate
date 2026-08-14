#!/usr/bin/env bash
# posttooluse-cache-clean.sh — PostToolUse Bash janitor. Makes "reclaim the
# local caches after every push" hold regardless of HOW the push happened, and
# backstops it with a free-disk floor.
#
# WHY BOTH TRIGGERS:
#   1. PUSH. .claude/hooks/git-post-commit.sh runs the cleaner only inside its
#      own auto-push branch. Any push that does not go through that branch — a
#      manual `git push` tool call, or a retry after the auto-push aborted on a
#      rebase conflict — left the caches growing untouched. This hook closes
#      that path: every push cleans, no matter who issued it.
#   2. FREE-DISK FLOOR. Triggering only on pushes still means a long stretch of
#      work with no push accumulates without bound — the failure this exists to
#      prevent (a full disk breaking local development). Below the floor the
#      cleaner runs whether or not anything was pushed.
#
# The actual reclaim lives in post-push-clean-caches.sh; this file only decides
# WHEN to call it. It is a janitor, not an enforcement gate: it never blocks and
# never fails a tool call (PostToolUse fires after the work is already done, so
# a non-zero exit would report a failure that did not happen). Deliberately not
# fail-closed, and it does not source the fail-closed helper trap.
set -uo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

# Free-space floor in GiB. Below this, reclaim on any Bash call.
FLOOR_GB="${OPENGATE_DISK_FLOOR_GB:-40}"

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"

# Defer while a build is in flight. The gauntlet is routinely BACKGROUNDED while
# other Bash calls continue, and `cargo clean` against a live target dir
# corrupts the build it is racing. Deferring costs nothing: the next Bash call
# after the build finishes still sees the disk below the floor and reclaims.
# Matched against other processes' command lines — this hook's own name is not
# in the pattern, so it can never see itself.
build_in_flight() {
  command -v pgrep >/dev/null 2>&1 || return 1
  pgrep -f 'precommit-gauntlet\.sh|cargo (build|test|clippy|mutants)|go (build|test)|docker (build|compose)' \
    >/dev/null 2>&1
}

if build_in_flight; then
  exit 0
fi

should_clean=false

# Trigger 1 — a push verb, in any form (bare, flagged, `-c key=val`-prefixed,
# or chained after a `cd`). Each pre-verb token must be an option, optionally
# followed by its own value word — `-c` takes its value as a SEPARATE token
# (`git -c color.ui=false push`), so matching only `-`-prefixed tokens misses
# that form. Requiring an option lead keeps `git log --grep=push` from matching.
if printf '%s' "$cmd" \
  | grep -qE '\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+([^-][^[:space:]]*[[:space:]]+)?)*push\b'; then
  should_clean=true
fi

# Trigger 2 — free space under the floor. One `df` call, so this stays cheap
# enough to run after every Bash call.
if [ "$should_clean" = "false" ]; then
  avail_kb="$(df -Pk "$(project_root)" 2>/dev/null | awk 'NR==2 {print $4}')"
  if [ -n "${avail_kb:-}" ] && [ "$avail_kb" -lt $((FLOOR_GB * 1024 * 1024)) ] 2>/dev/null; then
    printf 'cache-clean: free space below %s GiB floor — reclaiming\n' "$FLOOR_GB" >&2
    should_clean=true
  fi
fi

[ "$should_clean" = "true" ] || exit 0

cleaner="$(dirname "${BASH_SOURCE[0]}")/post-push-clean-caches.sh"
if [ -x "$cleaner" ]; then
  "$cleaner" "$(project_root)" >&2 || true
fi

exit 0
