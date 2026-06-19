#!/usr/bin/env bash
# Convert canonical load-test rows to Prometheus text and push them to VM.
set -euo pipefail

SUMMARY_FILE="${1:-loadtest-summary.json}"

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

    def sample($metric; $value):
      select($value != null)
      | "\($metric){commit=\"\(.commit | label_escape)\",env=\"\((.env // "ci") | label_escape)\",source=\"\((.source // "unknown") | label_escape)\",scenario=\"\((.scenario // "unknown") | label_escape)\",phase=\"\((.phase // "aggregate") | label_escape)\"} \($value)";

    .[]
    | sample("loadtest_latency_p50_ms"; .latency_p50_ms),
      sample("loadtest_latency_p95_ms"; .latency_p95_ms),
      sample("loadtest_latency_p99_ms"; .latency_p99_ms),
      sample("loadtest_rps"; .rps),
      sample("loadtest_error_rate"; .error_rate)
  ' "$SUMMARY_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no load-test metrics generated from $SUMMARY_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
