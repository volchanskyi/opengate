#!/usr/bin/env bash
# Pushes the canonical mutation-test row to the in-cluster Loki Service through
# a throwaway curl pod. Loki remains private; no ingress route is required.
#
# Invoked by .github/workflows/mutation.yml after summarize and JSONL append.
# Per the PR 9 plan: .claude/plans/pr9-mutation-testing-as-observability.md
#
# Inputs:
#   $1   path to the canonical row JSON file (one object, single line)
# Loki receives one stream per language with labels
# {job="mutation-testing", language="<lang>", env="ci"} and a single log line
# whose value is the per-language `scores.<lang>` JSON.

set -euo pipefail

ROW_FILE="${1:?Usage: $0 <canonical-row.json>}"
[[ -f "$ROW_FILE" ]] || {
  echo "missing canonical row file: $ROW_FILE" >&2
  exit 2
}

# Loki accepts /loki/api/v1/push with `streams: [{stream, values: [[ns, line]]}]`.
# `ns` is a nanosecond epoch string. We push three streams (one per language)
# in a single request to minimize round-trips.
NS="$(date -u +%s)000000000"

PAYLOAD="$(jq -c \
  --arg ns "$NS" \
  --slurpfile row <(cat "$ROW_FILE") \
  '
  ($row[0]) as $r
  | {
      streams: [
        ["rust", $r.scores.rust],
        ["go",   $r.scores.go],
        ["web",  $r.scores.web]
      ]
      | map({
          stream: {
            job: "mutation-testing",
            language: .[0],
            env: "ci",
            commit: ($r.commit // "unknown")
          },
          values: [[ $ns, (.[1] | tostring) ]]
        })
    }
  ' <<<"{}")"

# Push the payload to Loki via the shared kubectl transport.
# shellcheck source=lib/loki-push.sh
# shellcheck source=lib/loki-push.sh
source "$(dirname "$0")/lib/loki-push.sh"
printf '%s' "$PAYLOAD" | loki_push
