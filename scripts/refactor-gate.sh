#!/usr/bin/env bash
# The two ends of /refactor; finish with nothing tracked uncommitted also marks HEAD for push.
#
# Usage:
#   refactor-gate.sh start   records the content /refactor begins on
#   refactor-gate.sh finish  spends a recorded start and records the content left
#
# Exit codes:
#   0  recorded
#   1  refused, the message naming the missing step
#   2  usage
set -euo pipefail

# shellcheck source=../.claude/hooks/lib/tidy-up.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.claude/hooks/lib/tidy-up.sh"

refuse() {
  printf 'refactor-gate: %s\n' "$1" >&2
  exit 1
}

tidy_root >/dev/null || refuse "not inside a git work tree."

case "${1:-}" in
  start)
    now="$(tidy_fingerprint)" || refuse "could not read the working tree."
    tidy_write refactor.start "$now"
    echo "refactor-gate: /refactor begins; the commit runs ./scripts/precommit-gauntlet.sh on what it leaves."
    ;;
  finish)
    started="$(tidy_read refactor.start)"
    [ -n "$started" ] \
      || refuse "no /refactor start is recorded. The order is 'scripts/refactor-gate.sh start', the tidy-up, 'scripts/refactor-gate.sh finish', then the commit, which runs ./scripts/precommit-gauntlet.sh."
    now="$(tidy_fingerprint)" || refuse "could not read the working tree."
    tidy_write refactor.done "$now"
    tidy_clear refactor.start
    echo "refactor-gate: /refactor finished; a commit of exactly this content may go ahead."
    if git diff --quiet HEAD -- 2>/dev/null; then
      tidy_write refactor.head "$(git rev-parse HEAD)"
      echo "refactor-gate: nothing tracked is uncommitted, so HEAD carries this content and may be pushed."
    fi
    ;;
  *)
    echo "usage: scripts/refactor-gate.sh start|finish" >&2
    exit 2
    ;;
esac
