#!/usr/bin/env bash
# pretooluse-test-value-guard.sh — refuse a web test that cannot fail for the
# reason it claims to.
#
# Triggers on PreToolUse Write|Edit|MultiEdit. It judges the file the tool call
# would produce — for an Edit that means replaying the replacement against what
# is on disk — and hands it to scripts/test-value-check.sh, the single source of
# truth for both this hook and the repo-wide sweep in
# scripts/tests/test-value.test.sh. Two shapes are refused:
#
#   1. A test that never binds the primary export of the module it is named
#      for — the shape of a test that copied production code into itself.
#   2. A global or prototype reassignment with no restore.
#
# Nothing here is shape-based: assertion form, styling assertions and DOM walks
# are deliberately not refused, for the reasons .claude/rules/test-value.md
# records. NO BYPASS — edit .claude/settings.json to change enforcement.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name tool_input.file_path tool_input.content \
  tool_input.old_string tool_input.new_string tool_input.replace_all tool_input.edits

tool="${HOOK_TOOL_NAME:-}"
case "$tool" in
  Write | Edit | MultiEdit) : ;;
  *) exit 0 ;;
esac

path="${HOOK_TOOL_INPUT_FILE_PATH:-}"
[ -n "$path" ] || exit 0

case "$path" in
  *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) : ;;
  *) exit 0 ;;
esac

ROOT="${TEST_VALUE_ROOT:-$PROJECT_ROOT}"
CHECK="$PROJECT_ROOT/scripts/test-value-check.sh"
[ -x "$CHECK" ] || exit 0

rel="${path#"$ROOT/"}"
rel="${rel#./}"

work="$(mktemp)"
stderr_file="$(mktemp)"
trap 'rm -f "$work" "$stderr_file"' EXIT

# Build the content the tool call would leave on disk. A Write carries it
# whole; an Edit or MultiEdit is replayed against the current file. A replay
# that does not apply means a tool call that would fail anyway, so the guard
# stands aside there and the sweep keeps the repo-wide line.
export HOOK_TOOL_INPUT_CONTENT HOOK_TOOL_INPUT_OLD_STRING HOOK_TOOL_INPUT_NEW_STRING
export HOOK_TOOL_INPUT_REPLACE_ALL HOOK_TOOL_INPUT_EDITS

build_rc=0
HOOK_TARGET="$ROOT/$rel" python3 - "$tool" "$work" <<'PYEOF' || build_rc=$?
import json, os, sys

tool, out = sys.argv[1], sys.argv[2]
target = os.environ["HOOK_TARGET"]


def current():
    if not os.path.isfile(target):
        return ""
    with open(target, encoding="utf-8", errors="replace") as fh:
        return fh.read()


def apply(text, old, new, replace_all):
    if text is None or old == "" or old not in text:
        return None
    return text.replace(old, new) if replace_all else text.replace(old, new, 1)


truthy = ("True", "true", "1")

if tool == "Write":
    content = os.environ.get("HOOK_TOOL_INPUT_CONTENT", "")
elif tool == "Edit":
    content = apply(
        current(),
        os.environ.get("HOOK_TOOL_INPUT_OLD_STRING", ""),
        os.environ.get("HOOK_TOOL_INPUT_NEW_STRING", ""),
        os.environ.get("HOOK_TOOL_INPUT_REPLACE_ALL", "") in truthy,
    )
else:
    content = current()
    try:
        edits = json.loads(os.environ.get("HOOK_TOOL_INPUT_EDITS", "") or "[]")
    except ValueError:
        edits = []
    for e in edits:
        content = apply(
            content,
            e.get("old_string", ""),
            e.get("new_string", ""),
            bool(e.get("replace_all")),
        )

if content is None:
    sys.exit(1)
with open(out, "w", encoding="utf-8") as fh:
    fh.write(content)
PYEOF

[ "$build_rc" -eq 0 ] || exit 0

if TEST_VALUE_ROOT="$ROOT" "$CHECK" file "$rel" "$work" 2>"$stderr_file"; then
  exit 0
fi

detail="$(head -3 "$stderr_file" | tr '\n' ' ')"
block test-value "Write/Edit refused: $detail See .claude/rules/test-value.md — a test asserts on the code that ships, and leaves the environment as it found it."
