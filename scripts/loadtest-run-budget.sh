#!/usr/bin/env bash
# Derives the generator pod lifetime and the verdict wait from the declared fleet hold and prints
# them as KEY=value lines for $GITHUB_ENV.
#
# Environment:
#   LOADTEST_HOLD                 how long the fleet is held, as a Go duration such as 8m (required)
#   LOADTEST_SETUP_SECONDS        room before the hold (default 600)
#   LOADTEST_TEARDOWN_SECONDS     room after it, before the pod may go (default 720)
#
# Usage:
#   LOADTEST_HOLD=8m scripts/loadtest-run-budget.sh >>"$GITHUB_ENV"
set -euo pipefail

SETUP_SECONDS="${LOADTEST_SETUP_SECONDS:-600}"
TEARDOWN_SECONDS="${LOADTEST_TEARDOWN_SECONDS:-720}"

: "${LOADTEST_HOLD:?LOADTEST_HOLD is required: every other figure here is derived from it}"

# duration_seconds parses a Go duration of hours, minutes and seconds, in that order, any subset.
duration_seconds() {
  local text="$1" total=0 number unit rest="$1"

  if ! grep -qE '^([0-9]+h)?([0-9]+m)?([0-9]+s)?$' <<<"$text" || [ -z "$text" ]; then
    echo "loadtest-run-budget: '$text' is not a duration the harness would accept (try 8m, 90s, 5h30m)" >&2
    return 2
  fi

  while [ -n "$rest" ]; do
    number="${rest%%[hms]*}"
    unit="${rest:${#number}:1}"
    rest="${rest:${#number}+1}"
    case "$unit" in
      h) total=$((total + number * 3600)) ;;
      m) total=$((total + number * 60)) ;;
      s) total=$((total + number)) ;;
    esac
  done

  if [ "$total" -le 0 ]; then
    echo "loadtest-run-budget: a hold of '$text' is no hold at all" >&2
    return 2
  fi
  printf '%s\n' "$total"
}

main() {
  local hold collect lifetime
  hold="$(duration_seconds "$LOADTEST_HOLD")"

  # The wait for the verdict begins after the k6 scenarios beside the fleet have run.
  collect=$((SETUP_SECONDS + hold))
  # The pod outlives the verdict by the room the bundle copy needs.
  lifetime=$((collect + TEARDOWN_SECONDS))

  printf 'LOADTEST_HOLD_SECONDS=%s\n' "$hold"
  printf 'LOADTEST_POD_LIFETIME_SECONDS=%s\n' "$lifetime"
  printf 'LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS=%s\n' "$collect"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
