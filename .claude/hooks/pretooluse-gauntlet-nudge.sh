#!/usr/bin/env bash
# Runs in the background (asyncRewake) and exits 2 after 10 minutes if HEAD has not advanced,
# waking the model to post a status update; any other outcome exits 0.
set -uo pipefail

# The settings filter already limits this hook to commits; the payload check guards a stray match.
payload="$(cat 2>/dev/null || true)"
case "$payload" in
  *'git commit'*) : ;;
  *) exit 0 ;;
esac

before="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
sleep 600
after="$(git rev-parse HEAD 2>/dev/null || echo unknown)"

if [ "$before" != "$after" ]; then
  exit 0
fi

echo "~10 minutes elapsed and HEAD has not advanced — the commit/gauntlet is likely still running. Verify with 'git log -1 --format=%h %s' and post a concise status update to the user (and re-check the background task output)."
exit 2
