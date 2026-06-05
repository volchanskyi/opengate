#!/usr/bin/env bash
# pretooluse-gauntlet-nudge.sh
#
# asyncRewake "nudge" hook. Wired in .claude/settings.json on PreToolUse/Bash with
# `if: "Bash(git commit:*)"`, so it fires only when a commit is launched — whose
# PreToolUse commit-guard then runs the ~10-minute precommit gauntlet.
#
# Because the settings entry sets "asyncRewake": true, this runs in the BACKGROUND
# (never blocks the turn). It waits ~10 minutes and, if the commit has NOT landed
# yet (HEAD unchanged), exits 2 to WAKE THE MODEL so it posts a status update to
# the user — enforcing the "update every 10 minutes" rule via the hook instead of
# an ad-hoc timer the model has to babysit.
#
# Single-shot per commit: one nudge at ~10 min. The gauntlet itself is ~10 min, so
# the background-task completion notification covers the end. Fail-safe: any error
# exits 0 (no spurious wake); it never blocks the foreground turn.
set -uo pipefail

# Drain the hook payload on stdin; only proceed for an actual `git commit`
# (defense-in-depth on top of the settings `if` filter — a stray match must never
# arm a 10-minute nudge against an unrelated Bash command).
payload="$(cat 2>/dev/null || true)"
case "$payload" in
  *'git commit'*) : ;;
  *) exit 0 ;;
esac

before="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
sleep 600
after="$(git rev-parse HEAD 2>/dev/null || echo unknown)"

# HEAD advanced ⇒ the commit landed within the window ⇒ no nudge needed.
if [ "$before" != "$after" ]; then
  exit 0
fi

echo "~10 minutes elapsed and HEAD has not advanced — the commit/gauntlet is likely still running. Verify with 'git log -1 --format=%h %s' and post a concise status update to the user (and re-check the background task output)."
exit 2
