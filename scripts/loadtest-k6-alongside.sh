#!/usr/bin/env bash
# Runs the browser-side scenarios beside a machine-side harness, signing the technicians in first.
# Each scenario starts once the estate is filed and the walk has a start; a timed-out wait fails.
#
# Environment:
#   LOADTEST_TECHNICIANS_READY  the file the harness waits for before its walk (required)
#   LOADTEST_K6_ALONGSIDE_TIMEOUT_SECONDS  how long to wait for each harness line (default 900)
#   LOADTEST_K6_ALONGSIDE_POLL_SECONDS  gap between looks (default 2)
#   plus everything scripts/loadtest-k6-run.sh reads
#
# Usage:
#   loadtest-k6-alongside.sh <harness-output-path> <scenario>...
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SIGN_IN_ANNOUNCEMENT='Technicians may sign in'
FILED_ANNOUNCEMENT='Estate filed'
WALK_ANNOUNCEMENT='Walk started at'
TIMEOUT="${LOADTEST_K6_ALONGSIDE_TIMEOUT_SECONDS:-900}"
POLL="${LOADTEST_K6_ALONGSIDE_POLL_SECONDS:-2}"

# The technicians' accounts, signed in for this run and removed with it.
ACCOUNTS=""

usage() {
  echo "usage: $0 <harness-output-path> <scenario>..." >&2
}

walk_started_at() {
  local log="$1" announced line
  [ -f "$log" ] || return 1
  announced="$(grep -F "$WALK_ANNOUNCEMENT" "$log" || true)"
  line="${announced##*$'\n'}"
  [[ "$line" =~ ([0-9]+)[[:space:]]*$ ]] || return 1
  printf '%s\n' "${BASH_REMATCH[1]}"
}

announced() {
  local log="$1" line="$2"
  [ -f "$log" ] || return 1
  grep -qF "$line" "$log"
}

await_sign_in() {
  local log="$1" deadline=$((SECONDS + TIMEOUT))
  while [ "$SECONDS" -lt "$deadline" ]; do
    announced "$log" "$SIGN_IN_ANNOUNCEMENT" && return 0
    sleep "$POLL"
  done
  echo "::error::the harness did not say the technicians may sign in within ${TIMEOUT}s, so no technician load was offered." >&2
  return 1
}

await_walk() {
  local log="$1" deadline=$((SECONDS + TIMEOUT)) started
  while [ "$SECONDS" -lt "$deadline" ]; do
    if announced "$log" "$FILED_ANNOUNCEMENT" && started="$(walk_started_at "$log")"; then
      printf '%s\n' "$started"
      return 0
    fi
    sleep "$POLL"
  done
  echo "::error::the harness did not file an estate and say when it started walking within ${TIMEOUT}s, so no technician load was offered and the leg varies its own variable against nothing." >&2
  return 1
}

# tell_harness writes the ready file whole, so the harness never reads half a word.
tell_harness() {
  local ready="$1" word="$2"
  printf '%s\n' "$word" >"$ready.writing"
  mv "$ready.writing" "$ready"
}

# sign_in signs every scenario's technicians in at once, each into a file of its own, and tells the
# harness how it went.
sign_in() {
  local ready="$1" accounts="$2"
  shift 2
  local -A signing=()
  local scenario refused=()
  for scenario in "$@"; do
    "$ROOT/scripts/loadtest-k6-run.sh" --sign-in "$scenario" "$accounts/$scenario.json" &
    signing["$scenario"]=$!
  done
  for scenario in "${!signing[@]}"; do
    wait "${signing[$scenario]}" || refused+=("$scenario")
  done
  if [ "${#refused[@]}" -gt 0 ]; then
    tell_harness "$ready" "refused: the technicians of ${refused[*]} did not sign in"
    echo "::error::the technicians of ${refused[*]} did not sign in, so the walk does not start." >&2
    return 1
  fi
  tell_harness "$ready" signed-in
}

main() {
  if [ "$#" -lt 2 ]; then
    usage
    return 2
  fi

  local log="$1"
  shift
  local ready="${LOADTEST_TECHNICIANS_READY:?LOADTEST_TECHNICIANS_READY must name the file the harness waits for}"

  await_sign_in "$log" || return 1
  ACCOUNTS="$(mktemp -d)"
  trap 'rm -rf "$ACCOUNTS"' EXIT
  sign_in "$ready" "$ACCOUNTS" "$@" || return 1

  local started
  started="$(await_walk "$log")" || return 1

  local elapsed=$(($(date +%s) - started))
  [ "$elapsed" -ge 0 ] || elapsed=0
  export LOADTEST_WALK_ELAPSED_SECONDS="$elapsed"
  echo "joining the walk ${elapsed}s in"

  # Every scenario runs at once, each presenting addresses from a block of its own, since the
  # profile describes the whole night.
  local -A running=()
  local scenario failed=0
  for scenario in "$@"; do
    LOADTEST_TECHNICIANS="$ACCOUNTS/$scenario.json" \
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
