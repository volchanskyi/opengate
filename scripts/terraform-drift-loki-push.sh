#!/usr/bin/env bash
# Pushes the canonical terraform-drift summary to Loki on the production VPS
# via the existing deploy SSH tunnel + monitoring docker network. Mirrors
# scripts/mutation-loki-push.sh — only the stream labels differ.
#
# Invoked by .github/workflows/terraform-drift.yml after a drift is detected
# (refresh-only exit code 2). The SSH `deploy-target` alias is configured by
# the .github/actions/oci-ssh-setup composite earlier in the workflow.
#
# Inputs:
#   $1   path to the drift summary JSON file (single-line object from terraform-drift-summarize.sh)
#
# Loki receives one stream labelled
#   {app="opengate", source="terraform-drift", env="ci"}
# with a single log line whose value is the drift-summary JSON.

set -euo pipefail

SUMMARY_FILE="${1:?Usage: $0 <drift-summary.json>}"
[[ -f "$SUMMARY_FILE" ]] || { echo "missing summary file: $SUMMARY_FILE" >&2; exit 2; }

NS="$(date -u +%s)000000000"

PAYLOAD="$(jq -c \
  --arg ns "$NS" \
  --slurpfile row <(cat "$SUMMARY_FILE") \
  '
  ($row[0]) as $r
  | {
      streams: [
        {
          stream: {
            app:    "opengate",
            source: "terraform-drift",
            env:    "ci",
            commit: ($r.commit // "unknown")
          },
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
