#!/usr/bin/env bash
# Asserts a mutation shard's tool wrote its report, the only evidence of work since the tool
# step swallows its exit code.
# Usage: scripts/assert-mutation-report.sh <tool> <report-path>

set -euo pipefail

TOOL="${1:-}"
REPORT="${2:-}"
if [ -z "$TOOL" ] || [ -z "$REPORT" ]; then
  echo "usage: scripts/assert-mutation-report.sh <tool> <report-path>" >&2
  exit 2
fi

if [ ! -s "$REPORT" ]; then
  echo "::error::assert-mutation-report: ${TOOL} wrote no report at ${REPORT}, so this shard measured nothing." \
    "${TOOL} reports a surviving mutant with a non-zero exit too, which is why the step above does not read its status;" \
    "the usual cause of an empty report is the baseline test suite failing before mutation starts, named in the ${TOOL} output above." >&2
  exit 1
fi

# A Stryker runner whose test filter matches nothing reports every mutant survived;
# a covered mutant with zero completed tests exposes it.
if [ "$TOOL" = "stryker" ]; then
  untested="$(jq '[.files[].mutants[]
    | select(.status == "Survived" and ((.coveredBy // []) | length) > 0 and .testsCompleted == 0)]
    | length' "$REPORT")"
  if [ "$untested" -gt 0 ]; then
    echo "::error::assert-mutation-report: stryker ran no tests against ${untested} mutants that tests cover, and reported each one survived." \
      "That is the test runner failing to select the tests, not the tests failing to kill the mutants, so this shard's score measures nothing." >&2
    exit 1
  fi
fi

echo "assert-mutation-report: ${TOOL} wrote ${REPORT}"
