#!/usr/bin/env bash
# Fetches the timed runs nobody has written up and puts them in front of the session; the write
# guards hold every project file until each has an outcome. A SessionStart hook never blocks.
set -uo pipefail

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "${CLAUDE_PROJECT_DIR:-$PWD}")"
cat >/dev/null 2>&1 || true

listing="$("$repo/scripts/nightly-review.sh" fetch 2>&1)"
due="$(cat "$repo/.claude/.markers/nightly.due" 2>/dev/null || echo unreachable)"

if [ -z "$due" ]; then
  msg="NIGHTLY REVIEW: every timed run is written up; nothing is due."
else
  msg="NIGHTLY REVIEW: run /nightly-review before changing any file. Until each item has a written outcome, the write guards refuse every change inside the project, and the commit guard refuses every commit.
$listing"
fi

MSG="$msg" python3 -c '
import json, os
print(json.dumps({"hookSpecificOutput": {"hookEventName": "SessionStart",
                                         "additionalContext": os.environ["MSG"]}}))
'
exit 0
