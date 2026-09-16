#!/usr/bin/env bash
# Turn a run's evidence bundle into the canonical rows the limits are read
# against.
#
# A profile's limits are read by one thing — scripts/loadtest-gate-check.sh —
# and what that reads is an array of rows keyed by source/scenario/phase. Those
# rows are built from a browser-side export joined to the machine-side harness's
# text output, which happens in the load test's publish step and nowhere else.
# Seven other profiles declare limits and run on venues that produce no such
# join: the throwaway stack has no browser-side generator on it at all, so every
# number those profiles are judged by was read by nothing.
#
# What those venues do produce is this bundle. So the rows come out of it here,
# and the one evaluator reads them unchanged — a second evaluator would be a
# second set of numbers to keep level, which is the defect the single home for
# the limits exists to prevent.
#
# Only the series a limit names are emitted. A bundle carries more than these —
# the handshake tail among them — and a row nobody reads is the decoration this
# work exists to remove; scripts/tests/loadtest-bundle-rows.test.sh holds the
# two sides level in both directions, so a profile that starts holding a new
# measurement fails there rather than silently reading nothing.
#
# A bundle it cannot read refuses rather than printing an empty array. An empty
# array is a night where every limit passed for want of anything to compare, and
# a caller cannot tell it apart from a clean one.
#
# Usage: loadtest-bundle-rows.sh <bundle.json> [rows.json]
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json> [rows.json]" >&2
}

# rows_from turns a bundle's observations into canonical rows.
#
# The mapping is one line per series a profile may name, because the two
# vocabularies are different on purpose: a bundle names what the harness
# measured, and a row names where in a night's shape the measurement sits.
rows_from() {
  jq -c '
    (.observations // []) as $observed
    | (reduce $observed[] as $o ({}; .[$o.series] = $o.value)) as $value
    | {
        source: "quic",
        scenario: "quic-agents",
        commit: (.run.commit // "unknown"),
        env: (.run.environment // "unknown"),
        timestamp: (.run.finished_at // null)
      } as $base
    | [
        ($base + {phase: "aggregate", error_rate: $value.aggregate_error_rate}),
        ($base + {phase: "connect", latency_p95_ms: $value.connect_p95_ms}),
        # Registration is the server figure, and a run the server did not answer
        # has none. The row is absent rather than nought, which is what lets the
        # limits on it fail the night instead of passing against a zero.
        #
        # The tail and the middle case travel together because they answer
        # different questions about the same queue. Where a venue is driven to
        # the largest fleet it has been shown to hold, two runs an hour apart
        # under identical load read tails of 5,773 and 9,443 ms with middle
        # cases of 239 and 255 — the tail there is the queue and only the middle
        # case is the write, so a leg that can name only the tail has nothing it
        # can hold the write path to.
        (if $value.register_p95_ms == null then empty
         else ($base + {phase: "register",
                        latency_p95_ms: $value.register_p95_ms}
                     + (if $value.register_p50_ms == null then {}
                        else {latency_p50_ms: $value.register_p50_ms} end))
         end)
      ]
    | map(select(
        (.error_rate != null) or (.latency_p50_ms != null) or (.latency_p95_ms != null)
      ))
  ' "$1"
}

main() {
  if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    usage
    return 2
  fi

  local bundle="$1" out="${2:-}"

  if [ ! -s "$bundle" ]; then
    echo "::error::there is no evidence bundle at $bundle, so the numbers this night is judged by cannot be read — which is not the same as no limit being breached." >&2
    return 2
  fi

  local rows
  if ! rows="$(rows_from "$bundle")"; then
    echo "::error::$bundle could not be read as an evidence bundle." >&2
    return 2
  fi

  # The two series every machine-side bundle states. A bundle missing either is
  # one this does not understand, and answering with the rows it did find would
  # hand the limits on the other a night they could not fail.
  local phase
  for phase in aggregate connect; do
    if [ "$(jq --arg p "$phase" '[.[] | select(.phase == $p)] | length' <<<"$rows")" -eq 0 ]; then
      echo "::error::$bundle states no $phase reading, so the limits held against it would be read against nothing." >&2
      return 2
    fi
  done

  if [ -n "$out" ]; then
    printf '%s\n' "$rows" >"$out"
    echo "read $(jq 'length' <<<"$rows") row(s) off $bundle into $out"
    return 0
  fi

  printf '%s\n' "$rows"
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
