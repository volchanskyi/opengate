#!/usr/bin/env bash
# Converts network-drill rows to Prometheus text and pushes them to VictoriaMetrics.
# Scenario and victim labels ride with every sample, as one metric differs per scenario.
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
    | "\(.metric){env=\"\((.env // "ci") | label_escape)\",scenario=\"\((.scenario // "unknown") | label_escape)\",victim=\"\((.victim // "unknown") | label_escape)\"} \(.value)"
  ' "$SUMMARY_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no network-drill metrics generated from $SUMMARY_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
