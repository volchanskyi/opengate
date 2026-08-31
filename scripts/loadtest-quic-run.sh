#!/usr/bin/env bash
# Run the QUIC agent harness and keep its output only when the run measured
# something.
#
# The k6 half has had this rule since a crashed scenario was found trending the
# two requests its setup managed. The QUIC half had none: whatever the harness
# left behind was read, so a harness that could not reach the server at all
# produced a file the summarizer turned into rows of zero — and zeroes pull the
# window median down, which makes the next genuinely slow night compare
# favourably and pass. One bad night quietly costs two.
#
# The rule is the same one, stated for this half:
#
#   * exit 0 — every agent connected. A measurement.
#   * exit 1 — some agents failed. Still a measurement: a fleet that half
#     connects is exactly what the trend exists to record, and the error rate
#     carries the finding.
#   * exit 2 — the harness finished and measured nothing. It ran to completion
#     and printed a full results block; what the block says is that no agent
#     arrived. Rows of zeroes, so the output goes the same way as an abort's —
#     but naming it an abort sends a reader looking for a crash that never
#     happened.
#   * anything else — the harness aborted. Its output describes the abort, so it
#     is discarded and the run is short a scenario.
#
# Output that does not carry the harness's own results block is discarded for
# the same reason, whatever the exit code said.
#
# The verdict is also written to `<output-path>.status`, because this runs in
# the background while the operator-side generator runs beside it: the shell
# that started it has exited by the time it finishes, so its exit code has
# nowhere to be returned to. A caller waits by watching for that file.
#
# Usage: loadtest-quic-run.sh <output-path> -- <harness command...>
set -euo pipefail

# QUIC_AGENT_FAILURES is the harness's exit code when some agents failed but the
# run completed.
QUIC_AGENT_FAILURES=1

# QUIC_MEASURED_NOTHING is the harness's exit code when the run completed and no
# agent arrived. The harness knows this because it classifies its own run, and
# the classification is what says whether there is anything here to trend.
QUIC_MEASURED_NOTHING=2

usage() {
  echo "usage: $0 <output-path> -- <harness command...>" >&2
}

# measured reports whether the output carries a completed run's results block.
# The harness prints it only after every agent has finished, so its presence is
# what separates a run from an abort.
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

  local status_file="$output.status"
  # A verdict left by an earlier attempt would otherwise be read as this run's.
  rm -f "$status_file"

  local status=0
  "$@" >"$output" 2>&1 || status=$?
  cat "$output"

  if [ "$status" = "$QUIC_MEASURED_NOTHING" ]; then
    rm -f "$output"
    echo "::error::the QUIC harness finished and measured nothing; its output is discarded so a run that connected nobody does not enter the trend." >&2
    printf '%s\n' "$status" >"$status_file"
    return "$status"
  fi

  if [ "$status" -ne 0 ] && [ "$status" -ne "$QUIC_AGENT_FAILURES" ]; then
    rm -f "$output"
    echo "::warning::QUIC harness aborted (exit $status); its output is discarded so the aborted run does not enter the trend." >&2
    printf '%s\n' "$status" >"$status_file"
    return "$status"
  fi

  if ! measured "$output"; then
    rm -f "$output"
    echo "::error::QUIC harness produced no results block; the run measured nothing and its output is discarded." >&2
    printf '2\n' >"$status_file"
    return 2
  fi

  printf '%s\n' "$status" >"$status_file"
  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
