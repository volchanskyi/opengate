#!/usr/bin/env bash
# pretooluse-test-skip-guard.sh — block introducing silently-skipped tests.
#
# Triggers on PreToolUse Write|Edit|MultiEdit. Tests must always run
# deterministically (.claude/rules/tests-determinism.md): a test that skips on a
# missing dependency, an environment flag, or a focus marker is a false green.
# This guard refuses new content that introduces a skip/ignore/focus marker in a
# test file, across all three languages:
#
#   Go   (*_test.go)              t.Skip( / t.Skipf( / t.SkipNow(
#   Web  (*.{test,spec}.{ts,tsx,js,jsx})
#                                 it|test|describe .skip/.skipIf/.only/.todo/.fixme,
#                                 and xit/xdescribe/xtest/fit/fdescribe(
#   Rust (*.rs)                   #[ignore] attribute
#
# Deterministic provisioning (e.g. internal/testpg auto-starts Postgres) is the
# sanctioned alternative to skipping. NO BYPASS — edit .claude/settings.json to
# change enforcement.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.file_path tool_input.content tool_input.new_string tool_input.edits

tool="${HOOK_TOOL_NAME:-}"
case "$tool" in
  Write | Edit | MultiEdit) : ;;
  *) exit 0 ;;
esac

path="${HOOK_TOOL_INPUT_FILE_PATH:-}"
[ -n "$path" ] || exit 0

# Select the banned pattern + a human label by file type. Empty pattern → the
# file is not a test file this guard covers, so allow.
pattern=""
label=""
case "$path" in
  *_test.go)
    pattern='\.Skip(f|Now)?\('
    label='a t.Skip/t.Skipf/t.SkipNow call'
    ;;
  *.test.ts | *.test.tsx | *.test.js | *.test.jsx | *.spec.ts | *.spec.tsx | *.spec.js | *.spec.jsx)
    pattern='\b(it|test|describe)\.(skip|skipIf|only|todo|fixme)\b|\b(xit|xdescribe|xtest|fit|fdescribe)[[:space:]]*\('
    label='a .skip/.only/.todo/.fixme or x-/f- focus marker'
    ;;
  *.rs)
    pattern='#\[[[:space:]]*ignore'
    label='a #[ignore] attribute'
    ;;
  *) exit 0 ;;
esac

new_content=""
case "$tool" in
  Write) new_content="${HOOK_TOOL_INPUT_CONTENT:-}" ;;
  Edit) new_content="${HOOK_TOOL_INPUT_NEW_STRING:-}" ;;
  MultiEdit) new_content="${HOOK_TOOL_INPUT_EDITS:-}" ;;
esac

[ -n "$new_content" ] || exit 0

if printf '%s' "$new_content" | grep -qE "$pattern"; then
  block test-skip "Write/Edit refused: introduces ${label} in $path. Tests must always run deterministically — no silent skips (.claude/rules/tests-determinism.md). Provision the dependency instead (e.g. internal/testpg auto-starts Postgres); for a focus marker, remove it."
fi

exit 0
