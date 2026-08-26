#!/usr/bin/env bash
# Run one k6 scenario and keep its summary export only when the run measured
# something.
#
# k6 writes --summary-export whatever happens, including when the script throws
# in setup() and the workload never starts. That export holds the handful of
# requests setup managed, and scripts/loadtest-summarize.sh turns any export it
# finds into a canonical trend row, which scripts/loadtest-vm-push.sh then
# stores. A crashed scenario therefore lands a row of two-request latency and a
# 50% error rate in the same series the regression check compares against, so
# the crash goes on distorting the window median long after it is fixed.
#
# A failed threshold is the opposite case: the workload ran and the fleet was
# slow, which is exactly what the trend exists to record. So the exit code
# decides whether an export is kept — 0 and 99 (thresholds failed) produced a
# measurement, anything else aborted the run.
#
# A breached threshold does not fail the scenario. Whether a mark is blocking is
# a property of the profile that declared it, evaluated against the stored rows
# once the run is complete, and a mark deliberately set tighter than the
# measurement's current spread — so that a real regression becomes visible once
# the generator and the target stop sharing processors — would otherwise fail
# every night from the day it was tightened. The breach is announced and written
# beside the export as `<scenario>.thresholds`, so the gate reads it rather than
# inferring it from an exit code that has already been consumed.
#
# Every identity the scenario creates is named after the run id, and k6 is handed
# that id explicitly. k6 runs in a pod on the cluster, which inherits nothing
# from the machine that started it, so an id left to be inherited never arrives:
# the generator falls back to a fixed word, every night asks the server for the
# same addresses, and the second night is refused as a duplicate.
#
# Usage: loadtest-k6-run.sh <scenario-name> <script-path>
set -euo pipefail

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

  # A breach record left by an earlier attempt would otherwise be read as this
  # run's.
  rm -f "$breach_path"

  "$K6_BIN" run \
    --summary-export "$export_path" \
    --summary-trend-stats "${K6_SUMMARY_TREND_STATS:-avg,min,med,p(50),p(95),p(99),max}" \
    --env "BASE_URL=${LOADTEST_BASE_URL:?LOADTEST_BASE_URL must be set}" \
    --env "LOADTEST_RUN_ID=${LOADTEST_RUN_ID:?LOADTEST_RUN_ID must be set}" \
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

  if [ "$status" -eq "$K6_THRESHOLDS_FAILED" ]; then
    printf '%s\n' "$scenario" >"$breach_path"
    echo "::warning::k6 scenario $scenario breached a threshold; the measurement is kept and the profile's gates decide whether it fails the run." >&2
  fi

  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
