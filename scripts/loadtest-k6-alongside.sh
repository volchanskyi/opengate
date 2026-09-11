#!/usr/bin/env bash
# Run the browser-side scenarios beside a machine-side harness that is already
# walking, on a stack this job brought up itself.
#
# The sweep families vary one thing and hold the rest still. What they were
# holding still did not include any technician load at all: no browser-side
# generator ran on this venue, so a profile could declare ten journeys a second
# and the sweep would vary processors against five hundred machines arriving,
# which is the cheapest thing the server does. That is why its curve was flat
# from one processor upwards — the load was too small to saturate even one.
#
# Two things have to be true before a scenario starts, and both are the
# harness's own account of itself rather than a guess about how long a fixture
# takes:
#
#   * the estate is filed. Each scenario picks the building it will read from
#     once, in its own setup, so one started against an unfiled fleet reads an
#     empty building for the whole of its run.
#   * the walk has a start time. A generator that began the profile's shape
#     again from its beginning would be a phase behind for the rest of the run,
#     holding its steady window open past the drain.
#
# A wait that times out fails. A sweep leg that quietly ran no technician load
# is the flat curve this exists to fix, arrived at a second way.
#
# Environment:
#   LOADTEST_K6_ALONGSIDE_TIMEOUT_SECONDS   how long to wait for the two lines
#                                           (default 900)
#   LOADTEST_K6_ALONGSIDE_POLL_SECONDS      gap between looks (default 2)
#   plus everything scripts/loadtest-k6-run.sh reads.
#
# Usage: loadtest-k6-alongside.sh <harness-output-path> <scenario>...
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

FILED_ANNOUNCEMENT='Estate filed'
WALK_ANNOUNCEMENT='Walk started at'
TIMEOUT="${LOADTEST_K6_ALONGSIDE_TIMEOUT_SECONDS:-900}"
POLL="${LOADTEST_K6_ALONGSIDE_POLL_SECONDS:-2}"

usage() {
  echo "usage: $0 <harness-output-path> <scenario>..." >&2
}

# walk_started_at prints the second the harness began walking, or nothing.
walk_started_at() {
  local log="$1" announced line
  [ -f "$log" ] || return 1
  announced="$(grep -F "$WALK_ANNOUNCEMENT" "$log" || true)"
  line="${announced##*$'\n'}"
  [[ "$line" =~ ([0-9]+)[[:space:]]*$ ]] || return 1
  printf '%s\n' "${BASH_REMATCH[1]}"
}

# estate_filed reports whether the harness has said its estate is filed.
estate_filed() {
  local log="$1"
  [ -f "$log" ] || return 1
  grep -qF "$FILED_ANNOUNCEMENT" "$log"
}

# await_walk waits for both lines and prints the walk's start time.
await_walk() {
  local log="$1" deadline=$((SECONDS + TIMEOUT)) started
  while [ "$SECONDS" -lt "$deadline" ]; do
    if estate_filed "$log" && started="$(walk_started_at "$log")"; then
      printf '%s\n' "$started"
      return 0
    fi
    sleep "$POLL"
  done
  echo "::error::the harness did not file an estate and say when it started walking within ${TIMEOUT}s, so no technician load was offered and the leg varies its own variable against nothing." >&2
  return 1
}

main() {
  if [ "$#" -lt 2 ]; then
    usage
    return 2
  fi

  local log="$1"
  shift

  local started
  started="$(await_walk "$log")" || return 1

  local elapsed=$(($(date +%s) - started))
  [ "$elapsed" -ge 0 ] || elapsed=0
  export LOADTEST_WALK_ELAPSED_SECONDS="$elapsed"
  echo "joining the walk ${elapsed}s in"

  # Every scenario at once, each presenting addresses from a block of its own,
  # because the profile describes one night rather than one scenario's night.
  local -A running=()
  local scenario failed=0
  for scenario in "$@"; do
    "$ROOT/scripts/loadtest-k6-run.sh" "$scenario" "$ROOT/load/k6/scenarios/$scenario.js" &
    running["$scenario"]=$!
  done
  for scenario in "${!running[@]}"; do
    if ! wait "${running[$scenario]}"; then
      echo "::error::k6 scenario $scenario did not finish; this leg is short of its technician numbers." >&2
      failed=1
    fi
  done
  return "$failed"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
