#!/usr/bin/env bash
# Convert canonical network-drill rows to Prometheus text and push them to
# VictoriaMetrics.
#
# The scenario and the victim ride with every sample, because the same metric
# means different things on different scenarios: a reconnect after a three
# minute outage and a reconnect after a re-addressing are not one series.
set -euo pipefail

SUMMARY_FILE="${1:-netdrill-summary.json}"

if [[ ! -f "$SUMMARY_FILE" ]]; then
  echo "missing: $SUMMARY_FILE" >&2
  exit 2
fi

metrics="$(
  jq -r '
    def label_escape:
      tostring
      | gsub("\\\\"; "\\\\")
      | gsub("\""; "\\\"");

    .[]
    | select(.value != null)
    | "\(.metric){commit=\"\(.commit | label_escape)\",env=\"\((.env // "ci") | label_escape)\",scenario=\"\((.scenario // "unknown") | label_escape)\",victim=\"\((.victim // "unknown") | label_escape)\"} \(.value)"
  ' "$SUMMARY_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no network-drill metrics generated from $SUMMARY_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
