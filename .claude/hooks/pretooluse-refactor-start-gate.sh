#!/usr/bin/env bash
# pretooluse-refactor-start-gate.sh — /refactor begins only on content every
# check has passed.
#
# Triggers on PreToolUse Skill; noop unless the skill is `refactor`. Runs
# scripts/refactor-gate.sh start, which refuses unless the gauntlet passed on
# the content on disk, and records the start when it did.
#
# A /refactor the user types is expanded without a Skill tool call, so the
# skill's own first step runs the same start. This hook is the refusal on the
# path where the agent invokes it, before the skill's instructions load.
#
# NO BYPASS.
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
