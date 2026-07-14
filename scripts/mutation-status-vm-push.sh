#!/usr/bin/env bash
# Convert mutation run/shard completeness to Prometheus text and push it to VM.
set -euo pipefail

STATUS_FILE="${1:?Usage: $0 <mutation-status.json>}"

if [[ ! -f "$STATUS_FILE" ]]; then
  echo "missing mutation status file: $STATUS_FILE" >&2
  exit 2
fi

if ! jq -e '
  type == "object"
  and (.commit | type == "string" and length > 0)
  and (.complete | type == "boolean")
  and (.shards | type == "object")
  and all(.shards[];
    (.complete | type == "boolean")
    and (.reason == "ok" or .reason == "missing" or .reason == "invalid"))
' "$STATUS_FILE" >/dev/null 2>&1; then
  echo "invalid mutation status file: $STATUS_FILE" >&2
  exit 2
fi

metrics="$(
  jq -r '
    def label_escape:
      tostring
      | gsub("\\\\"; "\\\\")
      | gsub("\""; "\\\"");
    def bit: if . then 1 else 0 end;

    . as $row
    | "mutation_run_complete{commit=\"\($row.commit | label_escape)\",env=\"ci\"} \($row.complete | bit)",
      ($row.shards
       | to_entries
       | sort_by(.key)[]
       | "mutation_shard_complete{commit=\"\($row.commit | label_escape)\",env=\"ci\",shard=\"\(.key | label_escape)\"} \(.value.complete | bit)")
  ' "$STATUS_FILE"
)"

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
