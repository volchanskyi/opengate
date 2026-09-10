#!/usr/bin/env bash
# How long the pods holding the generators are given, derived from the run.
#
# The pod holding the fleet was created with a fixed `sleep 1800` and the wait
# for its verdict with a fixed 1500 seconds, so thirty minutes was the longest
# run that could exist — a ceiling nothing in the workflow declared, sitting
# beside a profile asking for eight hours. A pod that goes away underneath a run
# does not report the run it cut short; it reports a harness that reached no
# verdict, which is the same thing a broken cluster says.
#
# So the pod's lifetime is the run's. One figure is declared — how long the
# fleet is held — and everything downstream is that figure plus the room the
# work either side of it takes:
#
#   * before the hold: the fixture is built and the fleet enrols, which is as
#     long as the fleet is large;
#   * after it: the k6 scenarios beside it finish, the harness writes its
#     bundle, and the run copies it out of the pod.
#
# The margins are deliberately generous, because the cost of one that is too
# small is a night with no measurement at all and the cost of one too large is a
# pod idling in a namespace the run already holds.
#
# Environment:
#   LOADTEST_HOLD                 how long the fleet is held, as a Go duration
#                                 such as 8m or 5h (required)
#   LOADTEST_SETUP_SECONDS        room before the hold (default 600)
#   LOADTEST_TEARDOWN_SECONDS     room after it, before the pod may go (default 720)
#
# It writes `KEY=value` lines, which is what a workflow step appends to
# $GITHUB_ENV:
#   LOADTEST_HOLD_SECONDS                  the declared hold, in seconds
#   LOADTEST_POD_LIFETIME_SECONDS          how long each generator pod sleeps
#   LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS  how long the verdict is waited for
#
# The hold travels as a number because it is the one figure everything else is
# derived from, and a caller comparing it against anything — the walk a profile
# declares, the timeout on the job — would otherwise have to parse the duration
# a second time.
#
# Usage:
#   LOADTEST_HOLD=8m scripts/loadtest-run-budget.sh >>"$GITHUB_ENV"
set -euo pipefail

SETUP_SECONDS="${LOADTEST_SETUP_SECONDS:-600}"
TEARDOWN_SECONDS="${LOADTEST_TEARDOWN_SECONDS:-720}"

: "${LOADTEST_HOLD:?LOADTEST_HOLD is required: every other figure here is derived from it}"

# duration_seconds reads the Go duration the harness is given, so the workflow
# and the harness are reading the same string rather than two spellings of one
# intention. Hours, minutes and seconds, in that order, any subset.
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

  # The wait for the verdict begins part way through the run — the k6 scenarios
  # beside the fleet have already run — so a bound covering the whole run from
  # its own start cannot expire on a harness that is still working.
  collect=$((SETUP_SECONDS + hold))
  # The pod outlives the verdict by the room the collection needs, or the
  # bundle is copied out of a pod that has already gone.
  lifetime=$((collect + TEARDOWN_SECONDS))

  printf 'LOADTEST_HOLD_SECONDS=%s\n' "$hold"
  printf 'LOADTEST_POD_LIFETIME_SECONDS=%s\n' "$lifetime"
  printf 'LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS=%s\n' "$collect"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
