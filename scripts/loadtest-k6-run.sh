#!/usr/bin/env bash
# Runs one k6 scenario and keeps its summary export only when the run measured something.
# Exit 0 and 99 (thresholds failed) keep the export; any other exit aborts the run and drops it.
#
# Usage:
#   loadtest-k6-run.sh <scenario-name> <script-path>
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/loadtest-profile.sh
. "$SCRIPT_DIR/lib/loadtest-profile.sh"

K6_BIN="${K6_BIN:-k6}"
K6_THRESHOLDS_FAILED=99

main() {
  if [ "$#" -ne 2 ]; then
    echo "usage: $0 <scenario-name> <script-path>" >&2
    return 2
  fi

  local scenario="$1" script="$2"
  local summary_dir="${LOADTEST_K6_SUMMARY_DIR:-loadtest-k6}"
  local export_path="$summary_dir/$scenario.json"
  local breach_path="$summary_dir/$scenario.thresholds"
  local status=0
  local phases

  # The walk's elapsed seconds let this generator join the shape where it is.
  phases="$(profile_phases \
    "${LOADTEST_PROFILE:?LOADTEST_PROFILE must name the profile whose load this offers}" \
    "${LOADTEST_WALK_ELAPSED_SECONDS:-0}")" || return 2

  # A breach record from an earlier attempt would read as this run's.
  rm -f "$breach_path"

  "$K6_BIN" run \
    --summary-export "$export_path" \
    --summary-trend-stats "${K6_SUMMARY_TREND_STATS:-avg,min,med,p(50),p(95),p(99),max,count}" \
    --env "BASE_URL=${LOADTEST_BASE_URL:?LOADTEST_BASE_URL must be set}" \
    --env "LOADTEST_RUN_ID=${LOADTEST_RUN_ID:?LOADTEST_RUN_ID must be set}" \
    --env "LOADTEST_SCENARIO=$scenario" \
    --env "LOADTEST_PHASES=$phases" \
    "$script" || status=$?

  if [ "$status" -ne 0 ] && [ "$status" -ne "$K6_THRESHOLDS_FAILED" ]; then
    rm -f "$export_path"
    echo "::warning::k6 scenario $scenario aborted (exit $status); its summary export is discarded so the aborted run does not enter the trend." >&2
    return "$status"
  fi

  if [ ! -s "$export_path" ]; then
    echo "::error::k6 scenario $scenario wrote no summary export at $export_path" >&2
    return 2
  fi

  # A threshold breach is a notice, since the saturated legs cross k6's marks by design.
  if [ "$status" -eq "$K6_THRESHOLDS_FAILED" ]; then
    printf '%s\n' "$scenario" >"$breach_path"
    echo "::notice::k6 scenario $scenario crossed one of k6's own thresholds; the measurement is kept and the profile's gates decide whether it fails the run." >&2
    if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
      printf -- '- k6 scenario %s crossed one of its own thresholds; the profile'"'"'s gates judge the run.\n' \
        "$scenario" >>"$GITHUB_STEP_SUMMARY" 2>/dev/null || true
    fi
  fi

  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
