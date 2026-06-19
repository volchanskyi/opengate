#!/usr/bin/env bash
# Convert canonical benchmark rows to Prometheus text and push them to VM.
set -euo pipefail

ROWS_FILE="${1:-benchmark-rows.json}"

if [[ ! -f "$ROWS_FILE" ]]; then
  echo "missing: $ROWS_FILE" >&2
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
      | "\($metric){commit=\"\(.commit | label_escape)\",env=\"\((.env // "ci") | label_escape)\",benchmark=\"\(.name | label_escape)\",lang=\"\(.lang | label_escape)\"} \($value)";

    .[]
    | sample("benchmark_ns_op"; .ns_op),
      sample("benchmark_allocs_op"; .allocs_op),
      sample("benchmark_bytes_op"; .bytes_op)
  ' "$ROWS_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no benchmark metrics generated from $ROWS_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
