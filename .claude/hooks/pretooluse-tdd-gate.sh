#!/usr/bin/env bash
# pretooluse-tdd-gate.sh — primary TDD enforcement.
#
# Triggers on PreToolUse for Write|Edit|MultiEdit. Blocks the first edit to
# a source-language file on a branch that has no test changes yet.
#
# Source classifier and branch-has-test-change live in scripts/tdd-check.sh,
# shared with the commit-guard backup check and any future CI mirror.
#
# NO BYPASS. Honors no environment variable.
set -euo pipefail
# shellcheck source=lib/common.sh
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

parse_input_fields tool_name tool_input.file_path

# Only consider Write/Edit/MultiEdit. Other tools: noop allow.
case "${HOOK_TOOL_NAME:-}" in
  Write | Edit | MultiEdit) : ;;
  *) exit 0 ;;
esac

path="${HOOK_TOOL_INPUT_FILE_PATH:-}"
[ -n "$path" ] || exit 0

# Doc / config / generated files are not source — allow.
if ! is_source_path "$path"; then
  exit 0
fi

# Source file. Require a test change on the branch.
if branch_has_test_change; then
  exit 0
fi

base="$(git merge-base HEAD origin/dev 2>/dev/null \
  || git merge-base HEAD dev 2>/dev/null \
  || git merge-base HEAD origin/main 2>/dev/null \
  || git merge-base HEAD main 2>/dev/null \
  || git rev-list --max-parents=0 HEAD 2>/dev/null | head -1 \
  || echo unknown)"

msg=$(
  cat <<EOF
TDD violation. Per .claude/rules/tdd.md, the failing test MUST be written BEFORE the source code.
This branch (since ${base}) has no test files modified, added, or staged. Before editing ${path}, do ONE of:
  - add a new test file (e.g. server/internal/<pkg>/*_test.go, agent/.../tests/, web/src/**/__tests__/),
  - or extend an existing test file with a NEW failing assertion that covers the change.
Once a test exists on the branch this hook will not fire again. There is NO bypass.
EOF
)

block tdd-test-first "$msg"
