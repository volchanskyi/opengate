#!/usr/bin/env bash
# Pushes each leg's bundle rows into the trend, leaving out and naming a leg that measured nothing.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
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

  local bundle leg workload verdict rows metrics=""
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
    workload="$(jq -r '"\(.run.profile_name)/\(.run.profile_version)"' "$bundle")"
    "$HERE/loadtest-bundle-rows.sh" "$bundle" "$rows" >/dev/null
    metrics+="$(
      jq -r --arg leg "$leg" --arg workload "$workload" '
        def label_escape: tostring | gsub("\\\\"; "\\\\") | gsub("\""; "\\\"");
        def sample($metric; $value):
          select($value != null)
          | "\($metric){env=\"ci\",leg=\"\($leg | label_escape)\",phase=\"\(.phase | label_escape)\",workload=\"\($workload | label_escape)\"} \($value)";
        .[]
        | sample("perf_latency_p95_ms"; .latency_p95_ms),
          sample("perf_latency_p50_ms"; .latency_p50_ms),
          sample("perf_error_rate"; .error_rate)
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
