#!/usr/bin/env bash
# Runs the cache cleaner after any push and whenever free disk falls below the floor.
# It never fails a tool call, since PostToolUse fires after the work is already done.
set -uo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

FLOOR_GB="${OPENGATE_DISK_FLOOR_GB:-40}"

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"

# Cleaning waits for a build, since `cargo clean` on a live target dir corrupts it.
# The pattern omits this hook's own name, so the hook never matches itself.
build_in_flight() {
  command -v pgrep >/dev/null 2>&1 || return 1
  pgrep -f 'precommit-gauntlet\.sh|cargo (build|test|clippy|mutants)|go (build|test)|docker (build|compose)' \
    >/dev/null 2>&1
}

if build_in_flight; then
  exit 0
fi

should_clean=false

# Matches a push verb in any form, including `git -c color.ui=false push`.
if grep -qE "$(git_verb_re push)" <<<"$cmd"; then
  should_clean=true
fi

# One `df` call keeps the floor check cheap enough to run after every Bash call.
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
