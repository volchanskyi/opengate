#!/usr/bin/env bash
# Runs the QUIC agent harness and keeps its output only when the run measured something.
# Exit 0 and 1 keep the output, 2 (no agent arrived) and any abort discard it.
#
# Usage:
#   loadtest-quic-run.sh <output-path> -- <harness command...>
set -euo pipefail

# QUIC_AGENT_FAILURES is the harness's exit code when some agents failed but the run completed.
QUIC_AGENT_FAILURES=1

# QUIC_MEASURED_NOTHING is the harness's exit code when the run completed and no agent arrived.
QUIC_MEASURED_NOTHING=2

usage() {
  echo "usage: $0 <output-path> -- <harness command...>" >&2
}

# measured reports whether the output carries a completed run's results block, which the
# harness prints only after every agent has finished.
measured() {
  local path="$1"
  [ -s "$path" ] || return 1
  grep -q '^=== Results ===' "$path" || return 1
  grep -qE '^Agents:[[:space:]]+[0-9]+/[0-9]+[[:space:]]+succeeded$' "$path"
}

main() {
  if [ "$#" -lt 3 ]; then
    usage
    return 2
  fi

  local output="$1"
  shift
  if [ "$1" != "--" ]; then
    usage
    return 2
  fi
  shift

  local status=0
  "$@" >"$output" 2>&1 || status=$?
  cat "$output"

  if [ "$status" = "$QUIC_MEASURED_NOTHING" ]; then
    rm -f "$output"
    echo "::error::the QUIC harness finished and measured nothing; its output is discarded so a run that connected nobody does not enter the trend." >&2
    return "$status"
  fi

  if [ "$status" -ne 0 ] && [ "$status" -ne "$QUIC_AGENT_FAILURES" ]; then
    rm -f "$output"
    echo "::warning::QUIC harness aborted (exit $status); its output is discarded so the aborted run does not enter the trend." >&2
    return "$status"
  fi

  if ! measured "$output"; then
    rm -f "$output"
    echo "::error::QUIC harness produced no results block; the run measured nothing and its output is discarded." >&2
    return 2
  fi

  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
