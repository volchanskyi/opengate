#!/usr/bin/env bash
# Pushes the canonical mutation-test row to Loki on the production VPS via
# the existing deploy SSH tunnel + monitoring docker network. No new ingress
# route or Caddy auth needed — runs `curl` from a throwaway container attached
# to the Compose monitoring network so it can resolve `loki:3100`.
#
# Invoked by .github/workflows/mutation.yml after summarize and JSONL append.
# Per the PR 9 plan: .claude/plans/pr9-mutation-testing-as-observability.md
#
# Inputs:
#   $1   path to the canonical row JSON file (one object, single line)
# Required env:
#   DEPLOY_SSH_PRIVATE_KEY   private key for the deploy user (already set by
#                            .github/actions/oci-ssh-setup in the workflow)
#   DEPLOY_HOST              ssh target hostname (also set by oci-ssh-setup)
#
# Loki receives one stream per language with labels
# {job="mutation-testing", language="<lang>", env="ci"} and a single log line
# whose value is the per-language `scores.<lang>` JSON.

set -euo pipefail

ROW_FILE="${1:?Usage: $0 <canonical-row.json>}"
[[ -f "$ROW_FILE" ]] || { echo "missing canonical row file: $ROW_FILE" >&2; exit 2; }

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
  ' <<< "{}")"

# Push the payload (stdin) to Loki via the shared transport. The default
# (LOKI_PUSH_MODE=ssh-docker) keeps the pre-cutover SSH + docker-run path on the
# compose monitoring network; the cutover sets LOKI_PUSH_MODE=kubectl (ADR-030).
# shellcheck source=lib/loki-push.sh
source "$(dirname "$0")/lib/loki-push.sh"
printf '%s' "$PAYLOAD" | loki_push
