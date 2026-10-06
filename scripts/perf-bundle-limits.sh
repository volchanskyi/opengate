#!/usr/bin/env bash
# Reads a profile's limits against a run's evidence bundle; an invalid run's numbers print and
# decide nothing, and a bundle that is unreadable or carries no verdict fails.
#
# Exit codes:
#   0  nothing blocking to report
#   1  a run that measured the system crossed a limit it is held to
#   2  the question could not be asked
#
# Usage: perf-bundle-limits.sh <profile.yaml> <bundle.json> [rows.json]
set -euo pipefail

usage() {
  echo "usage: $0 <profile.yaml> <bundle.json> [rows.json]" >&2
}

main() {
  if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    usage
    return 2
  fi

  local profile="$1" bundle="$2"
  local rows="${3:-/tmp/perf-bundle-rows.json}"

  if [ ! -s "$bundle" ]; then
    echo "::error::no evidence bundle at $bundle, so whether this run measured anything is unknown." >&2
    return 2
  fi

  local verdict
  verdict="$(jq -r '.verdict.result // empty' "$bundle" 2>/dev/null || true)"
  if [ -z "$verdict" ]; then
    echo "::error::the bundle at $bundle carries no verdict, so whether this run measured anything is unknown." >&2
    return 2
  fi

  local here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  "$here/loadtest-bundle-rows.sh" "$bundle" "$rows"

  # Taken on the command's own line so errexit tests the command and keeps the script running.
  local status=0
  "$here/loadtest-gate-check.sh" "$profile" "$rows" || status=$?

  # A status of 2 or more is a setup defect, and says so on its own account.
  if [ "$status" -ge 2 ]; then
    return "$status"
  fi

  if [ "$verdict" = "invalid" ]; then
    if [ "$status" -ne 0 ]; then
      echo "::warning::the limits above were read against a run that did not measure the system, so they decide nothing; the run's own verdict is what this leg is red for." >&2
    fi
    return 0
  fi

  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
