#!/usr/bin/env bash
# pmat-loki-push.sh — push the canonical pmat-trend row to Loki on the
# production VPS via the deploy SSH tunnel + monitoring docker network.
# Mirrors scripts/mutation-loki-push.sh. Invoked by .github/workflows/
# pmat-trend.yml after summarize.
#
# Inputs:
#   $1   path to the canonical row JSON file (one object, single line)
# Required env (set by .github/actions/oci-ssh-setup in the workflow):
#   DEPLOY_SSH_PRIVATE_KEY, DEPLOY_HOST
#
# ONE stream with labels {job="pmat-trend", env="ci"} whose log line is the
# full row JSON. Labels are intentionally low-cardinality (no commit/grade) so
# pmat-loki-query.sh can read the latest value as a single series; commit and
# repo_grade live inside the JSON line and are recovered via `| json`.
set -euo pipefail

ROW_FILE="${1:?Usage: $0 <canonical-row.json>}"
[[ -f "$ROW_FILE" ]] || { echo "missing canonical row file: $ROW_FILE" >&2; exit 2; }

NS="$(date -u +%s)000000000"

PAYLOAD="$(jq -c \
  --arg ns "$NS" \
  --slurpfile row <(cat "$ROW_FILE") \
  '
  ($row[0]) as $r
  | {
      streams: [
        {
          stream: { job: "pmat-trend", env: "ci" },
          values: [[ $ns, ($r | tostring) ]]
        }
      ]
    }
  ' <<< "{}")"

# Push the payload (stdin) to Loki via the shared transport. Default
# (LOKI_PUSH_MODE=ssh-docker) keeps the pre-cutover path; cutover sets kubectl.
# shellcheck source=lib/loki-push.sh
source "$(dirname "$0")/lib/loki-push.sh"
printf '%s' "$PAYLOAD" | loki_push
