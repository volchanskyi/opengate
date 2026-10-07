#!/usr/bin/env bash
# Turns a run's evidence bundle into the canonical rows that loadtest-gate-check.sh reads.
# Only the series a limit names are emitted; an unreadable bundle fails, as an empty array passes.
#
# Usage:
#   loadtest-bundle-rows.sh <bundle.json> [rows.json]
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json> [rows.json]" >&2
}

# rows_from turns a bundle's observations into canonical rows: a bundle names what the harness
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
        # A run the server did not answer has no registration row, so its limits fail the night.
        # The p50 rides along: at the largest fleet the p95 is the queue and the p50 the write.
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

  # Every machine-side bundle states both series; answering with one would hand the other's
  # limits a night they cannot fail.
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
