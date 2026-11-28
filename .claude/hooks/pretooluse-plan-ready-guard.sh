#!/usr/bin/env bash
# Judges the plan a Write, Edit or MultiEdit would leave under .claude/plans/: every plan states its
# status, an active plan holds nothing open, and an active plan changes only by a whole Write.
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
case "$path" in
  .claude/plans/*.md | */.claude/plans/*.md) : ;;
  *) exit 0 ;;
esac
case "$path" in
  */.claude/plans/*/*) exit 0 ;;
esac

work="$(mktemp)"
before="$(mktemp)"
trap 'rm -f "$work" "$before"' EXIT
[ ! -f "$path" ] || cp "$path" "$before"

# Builds the content the call would leave on disk; an edit that fails to apply is allowed through
# because the tool call fails anyway.
export HOOK_TOOL_INPUT_CONTENT HOOK_TOOL_INPUT_OLD_STRING HOOK_TOOL_INPUT_NEW_STRING
export HOOK_TOOL_INPUT_REPLACE_ALL HOOK_TOOL_INPUT_EDITS
build_rc=0
python3 - "$tool" "$before" "$work" <<'PYEOF' || build_rc=$?
import json, os, sys

tool, before, out = sys.argv[1], sys.argv[2], sys.argv[3]
with open(before, encoding="utf-8", errors="replace") as fh:
    text = fh.read()


def apply(text, old, new, replace_all):
    if text is None or old == "" or old not in text:
        return None
    return text.replace(old, new) if replace_all else text.replace(old, new, 1)


if tool == "Write":
    text = os.environ.get("HOOK_TOOL_INPUT_CONTENT", "")
elif tool == "Edit":
    text = apply(text, os.environ.get("HOOK_TOOL_INPUT_OLD_STRING", ""),
                 os.environ.get("HOOK_TOOL_INPUT_NEW_STRING", ""),
                 os.environ.get("HOOK_TOOL_INPUT_REPLACE_ALL", "") in ("True", "true", "1"))
else:
    try:
        edits = json.loads(os.environ.get("HOOK_TOOL_INPUT_EDITS", "") or "[]")
    except ValueError:
        edits = []
    for e in edits:
        text = apply(text, e.get("old_string", ""), e.get("new_string", ""), bool(e.get("replace_all")))
if text is None:
    sys.exit(1)
with open(out, "w", encoding="utf-8") as fh:
    fh.write(text)
PYEOF
[ "$build_rc" -eq 0 ] || exit 0

rule="the plan rules in .claude/rules/plans-and-adrs.md"
status="$(plan_status "$work")"
case "$status" in
  draft | active | parked | blocked) : ;;
  *) block plan-status "Write/Edit refused: $path has no status line. The first line under its title reads 'Status: draft', 'active', 'parked' or 'blocked', then the reason after a full stop. See $rule." ;;
esac

if [ "$tool" != "Write" ] && { [ "$status" = "active" ] || [ "$(plan_status "$before")" = "active" ]; }; then
  block plan-active-whole "$tool refused: $path is active, and an active plan is rewritten whole with Write, so the check reads all of it. See $rule."
fi

[ "$status" = "active" ] || exit 0

# Code, quoted text and link targets are dropped first: a question mark or an open word there is
# a thing the plan names.
open_lines="$(
  python3 - "$work" <<'PYEOF'
import re, sys

with open(sys.argv[1], encoding="utf-8", errors="replace") as fh:
    text = fh.read()
text = re.sub(r"```.*?```", "", text, flags=re.S)
OPEN_WORDS = re.compile(r"not yet answered|to be decided|undecided|\bTBD\b|open question", re.I)
for number, line in enumerate(text.splitlines(), 1):
    plain = re.sub(r"`[^`]*`", "", line)
    plain = re.sub(r"\]\([^)]*\)", "]", plain)
    plain = re.sub(r"https?://\S+", "", plain)
    plain = re.sub(r"\"[^\"]*\"|“[^”]*”", "", plain)
    if plain.lstrip().startswith("#") and re.search(r"open question", plain, re.I):
        print(f"line {number}: an open-question heading: {line.strip()}")
    elif "?" in plain:
        print(f"line {number}: a question: {line.strip()}")
    elif OPEN_WORDS.search(plain):
        print(f"line {number}: an open word: {line.strip()}")
PYEOF
)"

if [ -n "$open_lines" ]; then
  block plan-ready "Write refused: $path is set active while it holds something open. Settle each with Ivan in a modal, or keep the plan at 'Status: draft'.
$open_lines
See $rule."
fi

exit 0
