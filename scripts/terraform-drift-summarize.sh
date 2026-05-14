#!/usr/bin/env bash
# Parses the JSON output of `terraform show -json drift.tfplan` (where
# `drift.tfplan` came from `terraform plan -refresh-only -detailed-exitcode`)
# and emits a single canonical drift record.
#
# Invoked by .github/workflows/terraform-drift.yml when refresh-only returns
# exit code 2 (drift detected). Mirrors the style of scripts/mutation-summarize.sh.
#
# Input:
#   $1   path to the terraform-show JSON
# Required env (optional):
#   GITHUB_SHA      tagged into the output record
#   GITHUB_RUN_ID   tagged into the output record
#
# Output (stdout): one JSON object on one line, e.g.
#   {"timestamp":"2026-...","run_id":"...","commit":"...","drift_count":3,
#    "resource_changes":[{"address":"module.networking.oci_core_security_list.opengate","actions":["update"],"type":"oci_core_security_list"}],
#    "summary":"3 resources drifted: 2 update, 1 delete"}
#
# Exit codes:
#   0  parsed successfully
#   2  input file missing or unparseable

set -euo pipefail

PLAN_JSON="${1:?Usage: $0 <drift.json>}"
[[ -f "$PLAN_JSON" ]] || { echo "missing plan json: $PLAN_JSON" >&2; exit 2; }

COMMIT="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
RUN_ID="${GITHUB_RUN_ID:-local}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# `terraform show -json` produces a top-level `resource_changes` array. Each
# element has `address`, `type`, and `change.actions` (array of strings).
# For refresh-only plans, `change.actions` typically contains "update", "create",
# "delete", or "no-op". Filter out no-ops; emit one record per drifted resource.
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
  ' "$PLAN_JSON" || { echo "parse failure on $PLAN_JSON" >&2; exit 2; }
