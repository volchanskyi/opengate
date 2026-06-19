#!/usr/bin/env bash
# Convert the canonical PMAT row to Prometheus text and push it to VM.
set -euo pipefail

ROW_FILE="${1:?Usage: $0 <canonical-row.json>}"

if [[ ! -f "$ROW_FILE" ]]; then
  echo "missing canonical row file: $ROW_FILE" >&2
  exit 2
fi

metrics="$(
  jq -r '
    def label_escape:
      tostring
      | gsub("\\\\"; "\\\\")
      | gsub("\""; "\\\"");

    def sample($row; $metric; $extra_labels; $value):
      select($value != null)
      | "\($metric){commit=\"\(($row.commit // "unknown") | label_escape)\",env=\"\(($row.env // "ci") | label_escape)\"\($extra_labels)} \($value)";

    . as $row
    | sample($row; "pmat_repo_score"; ",grade=\"\(($row.repo_grade // "?") | label_escape)\""; $row.repo_score),
      sample($row; "pmat_below_bplus"; ""; $row.below_bplus),
      (($row.categories // {}) | to_entries[] | sample($row; "pmat_category_score"; ",category=\"\(.key | label_escape)\""; .value))
  ' "$ROW_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no PMAT metrics generated from $ROW_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
