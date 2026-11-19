#!/usr/bin/env bash
# Runs one k6 scenario, keeping its summary export only on exit 0; the profile holds every limit,
# so the scripts declare no mark that can fail, and exit 99 means a script has grown one.
#
# Environment:
#   LOADTEST_TECHNICIANS  the accounts a sign-in step wrote for the scenario
#
# Usage:
#   loadtest-k6-run.sh <scenario-name> <script-path>
#   loadtest-k6-run.sh --sign-in <scenario-name> <accounts-path>
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/loadtest-profile.sh
. "$SCRIPT_DIR/lib/loadtest-profile.sh"

K6_BIN="${K6_BIN:-k6}"
K6_THRESHOLDS_FAILED=99

usage() {
  echo "usage: $0 <scenario-name> <script-path>" >&2
  echo "       $0 --sign-in <scenario-name> <accounts-path>" >&2
}

# walk prints the profile's walk from `elapsed` seconds in.
walk() { # elapsed
  profile_phases \
    "${LOADTEST_PROFILE:?LOADTEST_PROFILE must name the profile whose load this offers}" "$1"
}

# sign_in signs the scenario's technicians in, sized over the whole walk, and keeps their accounts
# at `accounts`; the run writes no summary export, which the trend would read as the scenario's.
sign_in() { # scenario accounts
  local scenario="$1" accounts="$2" phases status=0
  phases="$(walk 0)" || return 2
  "$K6_BIN" run \
    --env "BASE_URL=${LOADTEST_BASE_URL:?LOADTEST_BASE_URL must be set}" \
    --env "LOADTEST_RUN_ID=${LOADTEST_RUN_ID:?LOADTEST_RUN_ID must be set}" \
    --env "LOADTEST_SCENARIO=$scenario" \
    --env "LOADTEST_PHASES=$phases" \
    --env "LOADTEST_TECHNICIANS_OUT=$accounts" \
    "$SCRIPT_DIR/../load/k6/sign-in.js" || status=$?
  if [ "$status" -ne 0 ]; then
    echo "::error::the technicians of $scenario did not sign in (k6 exit $status)." >&2
    return "$status"
  fi
  if ! jq -e 'type == "array" and length > 0' "$accounts" >/dev/null 2>&1; then
    echo "::error::the sign-in for $scenario wrote no accounts at $accounts." >&2
    return 2
  fi
}

main() {
  if [ "${1:-}" = --sign-in ]; then
    [ "$#" -eq 3 ] || {
      usage
      return 2
    }
    sign_in "$2" "$3"
    return
  fi
  if [ "$#" -ne 2 ]; then
    usage
    return 2
  fi

  local scenario="$1" script="$2"
  local summary_dir="${LOADTEST_K6_SUMMARY_DIR:-loadtest-k6}"
  local export_path="$summary_dir/$scenario.json"
  local status=0
  local phases
  local signed_in=()
  [ -z "${LOADTEST_TECHNICIANS:-}" ] || signed_in=(--env "LOADTEST_TECHNICIANS=$LOADTEST_TECHNICIANS")

  # The walk's elapsed seconds let this generator join the shape where it is.
  phases="$(walk "${LOADTEST_WALK_ELAPSED_SECONDS:-0}")" || return 2

  "$K6_BIN" run \
    --summary-export "$export_path" \
    --summary-trend-stats "${K6_SUMMARY_TREND_STATS:-avg,min,med,p(50),p(95),p(99),max,count}" \
    --env "BASE_URL=${LOADTEST_BASE_URL:?LOADTEST_BASE_URL must be set}" \
    --env "LOADTEST_RUN_ID=${LOADTEST_RUN_ID:?LOADTEST_RUN_ID must be set}" \
    --env "LOADTEST_SCENARIO=$scenario" \
    --env "LOADTEST_PHASES=$phases" \
    "${signed_in[@]}" \
    "$script" || status=$?

  if [ "$status" -eq "$K6_THRESHOLDS_FAILED" ]; then
    rm -f "$export_path"
    echo "::error::k6 scenario $scenario failed one of its own marks, but these scripts declare none: the profile holds every limit. Its summary export is discarded." >&2
    return "$status"
  fi
  if [ "$status" -ne 0 ]; then
    rm -f "$export_path"
    echo "::warning::k6 scenario $scenario aborted (exit $status); its summary export is discarded so the aborted run does not enter the trend." >&2
    return "$status"
  fi

  if [ ! -s "$export_path" ]; then
    echo "::error::k6 scenario $scenario wrote no summary export at $export_path" >&2
    return 2
  fi

  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
