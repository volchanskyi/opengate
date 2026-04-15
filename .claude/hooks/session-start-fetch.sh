#!/usr/bin/env bash
# SessionStart hook: warn Claude if local HEAD is behind origin/dev.
# Silent when up-to-date or offline. Does not modify the working tree.
set -e

repo=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
cd "$repo"

branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
[ "$branch" = "dev" ] || exit 0

timeout 4 git fetch --quiet origin dev 2>/dev/null || exit 0

behind=$(git rev-list --count HEAD..origin/dev 2>/dev/null || echo 0)
[ "$behind" -gt 0 ] 2>/dev/null || exit 0

subjects=$(git log --format='%h %s' HEAD..origin/dev | head -10)
msg="Local HEAD is $behind commit(s) behind origin/dev. Upstream commits you have NOT pulled yet:
$subjects

Before reasoning about repo state (especially shared paths like deploy/, .github/, docs/), run: git pull --rebase origin dev"

# Emit JSON via python3 (portable, no jq dependency).
MSG="$msg" python3 -c '
import json, os, sys
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "SessionStart",
        "additionalContext": os.environ["MSG"],
    }
}))
'
