#!/usr/bin/env bash
# Read back what the run wrote about itself, and fail the step when it says the
# system was never measured.
#
# The harness classifies every run it finishes — valid, failed, or invalid — and
# puts the answer in the evidence bundle. Valid and failed are both
# measurements: one of a system that held, one of a system that did not. Invalid
# is the third, and it is the one that goes missing: the run did not measure the
# system at all.
#
# Nothing read it. The volume family passed a run whose bundle said "invalid" and
# whose fleet was 0 of 500 machines, so the sweep read as partly working when
# none of it was. This is the same shape as a cache save that warns and exits
# zero: the work was refused, the step is green, and the only way anyone finds
# out is by going to look.
#
# An absent or unreadable bundle fails here as loudly as an invalid one. The step
# runs on every path, including the one where the harness died before writing
# anything, and a guard that answers yes when it cannot ask is the false green it
# was written to close.
#
# Usage: perf-bundle-verdict.sh <bundle.json>
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json>" >&2
}

main() {
  if [ "$#" -ne 1 ]; then
    usage
    return 2
  fi

  local bundle="$1" result reasons
  if [ ! -s "$bundle" ]; then
    echo "::error::the run wrote no evidence bundle at $bundle, so what it measured is unknown." >&2
    return 1
  fi

  if ! result="$(jq -er '.verdict.result' "$bundle" 2>/dev/null)"; then
    echo "::error::$bundle carries no verdict, so whether the run measured anything is unknown." >&2
    return 1
  fi

  reasons="$(jq -r '.verdict.reasons // [] | .[]' "$bundle")"
  echo "run verdict: $result"

  # A run that measured nothing says why it did not, and that account belongs
  # beside the failure rather than in the output above it.
  if [ "$result" = "invalid" ]; then
    [ -z "$reasons" ] || printf '  %s\n' "$reasons" >&2
    echo "::error::this run did not measure the system, so its numbers describe nothing." >&2
    return 1
  fi

  [ -z "$reasons" ] || printf '  %s\n' "$reasons"
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
