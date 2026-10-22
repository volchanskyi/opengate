#!/usr/bin/env bash
# Pushes each leg's bundle rows into the trend, leaving out and naming a leg that measured nothing.
# A series scripts/perf-regression-check.sh flagged is pushed with flagged="1".
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   PERF_FLAGGED_FILE  the check's flagged series, one leg<TAB>phase<TAB>measurement a line
#
# Usage: perf-vm-push.sh <bundle.json>...   (a leg is named by the bundle's directory)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ROWS_FILE=""
cleanup() { [ -z "$ROWS_FILE" ] || rm -f "$ROWS_FILE"; }
trap cleanup EXIT

main() {
  if [ "$#" -eq 0 ]; then
    echo "usage: $0 <bundle.json>..." >&2
    return 2
  fi

  local bundle leg workload verdict rows metrics="" flagged='[]'
  if [ -n "${PERF_FLAGGED_FILE:-}" ] && [ -s "$PERF_FLAGGED_FILE" ]; then
    flagged="$(jq -Rnc '[inputs | split("\t") | select(length == 3)
      | {leg: .[0], phase: .[1], measurement: .[2]}]' <"$PERF_FLAGGED_FILE")"
  fi
  ROWS_FILE="$(mktemp)"
  rows="$ROWS_FILE"
  for bundle in "$@"; do
    if [ ! -s "$bundle" ]; then
      echo "::error::no evidence bundle at $bundle" >&2
      return 2
    fi
    leg="$(basename "$(dirname "$bundle")")"
    verdict="$(jq -r '.verdict.result // empty' "$bundle")"
    if [ "$verdict" = "invalid" ] || [ -z "$verdict" ]; then
      echo "::notice::leg $leg did not measure the system (verdict ${verdict:-absent}), so its rows stay out of the trend."
      continue
    fi
    workload="$(jq -r '"\(.run.profile_name)/\(.run.profile_version)/\(.run.profile_digest)"' "$bundle")"
    "$HERE/loadtest-bundle-rows.sh" "$bundle" "$rows" >/dev/null
    metrics+="$(
      jq -r --arg leg "$leg" --arg workload "$workload" --argjson flagged "$flagged" '
        def label_escape: tostring | gsub("\\\\"; "\\\\") | gsub("\""; "\\\"");
        def sample($measurement; $value):
          select($value != null)
          | . as $row
          | (if any($flagged[]; .leg == $leg and .phase == $row.phase and .measurement == $measurement)
              then "flagged=\"1\"," else "" end) as $flag
          | "perf_\($measurement){env=\"ci\",\($flag)leg=\"\($leg | label_escape)\",phase=\"\(.phase | label_escape)\",workload=\"\($workload | label_escape)\"} \($value)";
        .[]
        | sample("latency_p95_ms"; .latency_p95_ms),
          sample("latency_p50_ms"; .latency_p50_ms),
          sample("error_rate"; .error_rate)
      ' "$rows"
    )"$'\n'
  done

  if [ -z "${metrics//$'\n'/}" ]; then
    echo "::error::no leg measured the system, so there is nothing for the trend." >&2
    return 1
  fi

  # shellcheck source=lib/vm-push.sh
  . "$HERE/lib/vm-push.sh"
  printf '%s' "$metrics" | vm_push
}

main "$@"
