#!/usr/bin/env bash
# Convert the canonical mutation row to Prometheus text and push it to VM.
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

    def sample($row; $metric; $language; $value):
      select($value != null)
      | "\($metric){commit=\"\(($row.commit // "unknown") | label_escape)\",env=\"\(($row.env // "ci") | label_escape)\",language=\"\($language | label_escape)\"} \($value)";

    . as $row
    | ($row.scores // {} | to_entries[])
    | .key as $language
    | .value as $score
    | sample($row; "mutation_score"; $language; $score.score_pct),
      sample($row; "mutation_killed"; $language; $score.killed),
      sample($row; "mutation_survived"; $language; $score.survived),
      sample($row; "mutation_timeout"; $language; $score.timeout),
      sample($row; "mutation_no_coverage"; $language; $score.no_coverage),
      sample($row; "mutation_unviable"; $language; $score.unviable),
      sample($row; "mutation_total"; $language; $score.total)
  ' "$ROW_FILE"
)"

if [[ -z "$metrics" ]]; then
  echo "no mutation metrics generated from $ROW_FILE" >&2
  exit 2
fi

# shellcheck source=lib/vm-push.sh
source "$(dirname "$0")/lib/vm-push.sh"
printf '%s\n' "$metrics" | vm_push
