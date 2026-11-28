#!/usr/bin/env bash
# Runs in the background (asyncRewake) beside a gauntlet run and exits 2 after 10 minutes while the
# run is still going, waking the model to post a status update; any other outcome exits 0.
set -uo pipefail

# The settings filter limits this hook to the gauntlet; the payload check guards a stray match.
payload="$(cat 2>/dev/null || true)"
case "$payload" in
  *'precommit-gauntlet.sh'*) : ;;
  *) exit 0 ;;
esac

sleep 600

# The run's own command line names the script; this hook's names only itself.
if ! pgrep -f 'scripts/precommit-gauntlet[.]sh' >/dev/null 2>&1; then
  exit 0
fi

echo "~10 minutes elapsed and ./scripts/precommit-gauntlet.sh is still running. Read the tail of its log and post a concise status update to the user; on exit, a full pass writes .claude/.markers/gauntlet.pass."
exit 2
