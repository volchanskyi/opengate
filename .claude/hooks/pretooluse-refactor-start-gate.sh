#!/usr/bin/env bash
# Refuses a refactor Skill call unless scripts/refactor-gate.sh start passes on the content on disk.
# A typed /refactor expands without a Skill call, so the skill's first step runs the same start.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.skill

[ "${HOOK_TOOL_NAME:-}" = "Skill" ] || exit 0
case "${HOOK_TOOL_INPUT_SKILL:-}" in
  refactor | */refactor | *:refactor) : ;;
  *) exit 0 ;;
esac

if ! out="$("$PROJECT_ROOT/scripts/refactor-gate.sh" start 2>&1)"; then
  block refactor-before-gauntlet "/refactor refused: ${out#refactor-gate: }"
fi
exit 0
