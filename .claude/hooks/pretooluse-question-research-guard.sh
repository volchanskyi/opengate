#!/usr/bin/env bash
# Refuses a question to Ivan unless, since his last typed message, the session searched the web and
# searched this project's history; the session log is read at the hook input's transcript_path.
set -euo pipefail
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
enable_fail_closed_hook

parse_input_fields tool_name transcript_path

[ "${HOOK_TOOL_NAME:-}" = "AskUserQuestion" ] || exit 0

missing_rc=0
missing="$(
  python3 - "${HOOK_TRANSCRIPT_PATH:-}" <<'PYEOF'
import json, re, sys

path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as fh:
        entries = [json.loads(line) for line in fh if line.strip()]
except (OSError, ValueError) as err:
    print(f"the session log at '{path}' could not be read ({err})")
    sys.exit(3)

# The log also writes tool results, task notifications and summaries as user entries; only text
# Ivan typed counts as his message.
NOT_TYPED = ("<task-notification", "This session is being continued")


def typed(entry):
    if entry.get("type") != "user" or entry.get("isMeta") or entry.get("isSidechain"):
        return False
    content = (entry.get("message") or {}).get("content")
    if isinstance(content, str):
        return bool(content.strip()) and not content.startswith(NOT_TYPED)
    if not isinstance(content, list):
        return False
    kinds = {block.get("type") for block in content if isinstance(block, dict)}
    texts = [block.get("text", "") for block in content
             if isinstance(block, dict) and block.get("type") == "text"]
    return "tool_result" not in kinds and any(t.strip() and not t.startswith(NOT_TYPED) for t in texts)


last = max((i for i, entry in enumerate(entries) if typed(entry)), default=None)
if last is None:
    print("the session log holds no message Ivan typed")
    sys.exit(3)

HISTORY_RE = re.compile(r"\bgit\s+(log|show|blame)\b|\bgh\s+run\b|\bgh\s+api\b[^|;&\n]*\bactions\b")
web = history = False
for entry in entries[last + 1:]:
    if entry.get("type") != "assistant" or entry.get("isSidechain"):
        continue
    for block in (entry.get("message") or {}).get("content") or []:
        if not isinstance(block, dict) or block.get("type") != "tool_use":
            continue
        name, tool_input = block.get("name", ""), block.get("input") or {}
        if name in ("WebSearch", "WebFetch") or (
                name in ("Agent", "Task") and tool_input.get("subagent_type") == "web-fetch"):
            web = True
        if name in ("mcp__github__list_commits", "mcp__github__search_commits") or (
                name == "Bash" and HISTORY_RE.search(tool_input.get("command", ""))):
            history = True

gaps = []
if not web:
    gaps.append("a web search on how others do it (WebSearch, WebFetch or a web-fetch agent)")
if not history:
    gaps.append("a history search for an earlier attempt (git log, git show, git blame, gh run, "
                "gh api on /actions, or a GitHub commit search)")
print("; ".join(gaps))
PYEOF
)" || missing_rc=$?

if [ "$missing_rc" -ne 0 ]; then
  block question-research "Question refused: $missing. The check reads the log to find what was searched since Ivan's last message, so a log it cannot read refuses every question. .claude/rules/decision-questions.md."
fi

if [ -n "$missing" ]; then
  block question-research "Question refused: since Ivan's last message the session has not made $missing. Do both, then ask. .claude/rules/decision-questions.md."
fi

exit 0
