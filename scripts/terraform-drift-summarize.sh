#!/usr/bin/env bash
# Prints one single-line JSON drift record from `terraform show -json` of a refresh-only plan.
# The record carries timestamp, run_id, commit, drift_count, resource_changes and summary.
#
# Usage:
#   terraform-drift-summarize.sh <drift.json>
#
# Environment:
#   GITHUB_SHA      commit tagged into the record, default the current HEAD
#   GITHUB_RUN_ID   run id tagged into the record, default local
#
# Exit codes:
#   0  parsed successfully
#   2  input file missing or unparseable

set -euo pipefail

PLAN_JSON="${1:?Usage: $0 <drift.json>}"
[[ -f "$PLAN_JSON" ]] || {
  echo "missing plan json: $PLAN_JSON" >&2
  exit 2
}

COMMIT="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
RUN_ID="${GITHUB_RUN_ID:-local}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Changes whose only action is "no-op" are dropped; each remaining one is a drifted resource.
jq -e -c \
  --arg ts "$TIMESTAMP" \
  --arg run "$RUN_ID" \
  --arg sha "$COMMIT" \
  '
  ([.resource_changes // []]
   | flatten
   | map(select((.change.actions // []) | any(. != "no-op")))) as $changes
  | ($changes | map(.change.actions[0]) | group_by(.) | map({(.[0]): length}) | add // {}) as $by_action
  | ($by_action | to_entries | map("\(.value) \(.key)") | join(", ")) as $action_summary
  | {
      timestamp:        $ts,
      run_id:           $run,
      commit:           $sha,
      drift_count:      ($changes | length),
      resource_changes: ($changes | map({address: .address, actions: .change.actions, type: .type})),
      summary:          (
        if ($changes | length) == 0
        then "no drift detected"
        else "\($changes | length) resource(s) drifted: \($action_summary)"
        end
      )
    }
  ' "$PLAN_JSON" || {
  echo "parse failure on $PLAN_JSON" >&2
  exit 2
}
