#!/usr/bin/env bash
# Convert the canonical terraform-drift summary to Prometheus text and push it to VM.
set -euo pipefail

SUMMARY_FILE="${1:?Usage: $0 <drift-summary.json>}"

if [[ ! -f "$SUMMARY_FILE" ]]; then
  echo "missing summary file: $SUMMARY_FILE" >&2
  exit 2
fi

metrics="$(
  jq -r '
    def label_escape:
      tostring
      | gsub("\\\\"; "\\\\")
      | gsub("\""; "\\\"");

    def run_label($row):
      ",run_id=\"\(($row.run_id // "local") | label_escape)\"";

    def sample($row; $metric; $extra_labels; $value):
      select($value != null)
      | "\($metric){commit=\"\(($row.commit // "unknown") | label_escape)\",env=\"\(($row.env // "ci") | label_escape)\"\($extra_labels)} \($value)";

    . as $row
    | sample($row; "terraform_drift_count"; run_label($row); $row.drift_count),
      (
        ($row.resource_changes // [])
        | map(.type as $resource_type | (.actions // [])[] | { type: $resource_type, action: . })
        | sort_by(.type, .action)
        | group_by([.type, .action])[]
        | {
            type: .[0].type,
            action: .[0].action,
            count: length
          }
        | sample(
            $row;
            "terraform_drift_resources";
            "\(run_label($row)),action=\"\(.action | label_escape)\",type=\"\(.type | label_escape)\"";
            .count
          )
      )
  ' "$SUMMARY_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no terraform drift metrics generated from $SUMMARY_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
