#!/usr/bin/env bash
# Parses `terraform show -json` into a markdown summary for a PR comment and fails when a
# destroy targets a protected resource type, unless the caller asserts it is approved.
#
# Usage:
#   parse-tfplan.sh <tfplan.json> [--approve-destroy]
#
# Exit codes:
#   0  no destroy of a protected resource (or --approve-destroy supplied)
#   1  destroy of a protected resource detected without override
#   2  input file missing / unparseable

set -euo pipefail

PLAN_JSON="${1:?Usage: $0 <tfplan.json> [--approve-destroy]}"
APPROVE_DESTROY="0"
if [[ "${2:-}" == "--approve-destroy" ]]; then
  APPROVE_DESTROY="1"
fi

[[ -f "$PLAN_JSON" ]] || {
  echo "missing plan json: $PLAN_JSON" >&2
  exit 2
}

# Destroying these risks data loss, a networking outage or tfstate loss; the set only grows.
PROTECTED_TYPES=(
  oci_core_vcn
  oci_core_subnet
  oci_core_security_list
  oci_core_network_security_group
  oci_objectstorage_bucket
)

PROTECTED_JQ_LIST=$(printf '%s\n' "${PROTECTED_TYPES[@]}" | jq -R . | jq -s .)

SUMMARY="$(jq -e --argjson protected "$PROTECTED_JQ_LIST" -c '
  (.resource_changes // []) as $all
  | ($all | map(select((.change.actions // []) | index("create")))) as $adds
  | ($all | map(select((.change.actions // []) == ["update"]))) as $updates
  | ($all | map(select((.change.actions // []) | index("delete")))) as $deletes
  | {
      add:     ($adds | length),
      change:  ($updates | length),
      destroy: ($deletes | length),
      protected_destroys: ($deletes | map(select(.type as $t | $protected | index($t))))
    }
' "$PLAN_JSON")"

if [[ -z "$SUMMARY" ]]; then
  echo "parse failure on $PLAN_JSON" >&2
  exit 2
fi

ADD=$(jq -r '.add' <<<"$SUMMARY")
CHANGE=$(jq -r '.change' <<<"$SUMMARY")
DESTROY=$(jq -r '.destroy' <<<"$SUMMARY")
PROTECTED_DESTROY_COUNT=$(jq -r '.protected_destroys | length' <<<"$SUMMARY")

{
  echo "**Resource changes:** $ADD to add, $CHANGE to change, $DESTROY to destroy."
  echo ""
  if [[ "$PROTECTED_DESTROY_COUNT" -gt 0 ]]; then
    echo "### ⚠️ Destroys protected resource(s)"
    echo ""
    jq -r '.protected_destroys[] | "- `\(.type)` — `\(.address)`"' <<<"$SUMMARY"
    echo ""
    if [[ "$APPROVE_DESTROY" == "1" ]]; then
      echo "_The \`iac:approve-destroy\` label is present — destroy is operator-approved._"
    else
      echo "_Add the \`iac:approve-destroy\` label to authorize this destroy._"
    fi
    echo ""
  fi
  if [[ -f "$PLAN_JSON" ]]; then
    echo "<details><summary>Per-resource actions</summary>"
    echo ""
    jq -r '
      (.resource_changes // [])[]
      | select((.change.actions // []) | any(. != "no-op"))
      | "- " + (
          if (.change.actions | any(. == "delete")) then "❌"
          elif (.change.actions | any(. == "create")) then "➕"
          else "🔄"
          end
        ) + " `" + .address + "` (" + ((.change.actions // []) | join(",")) + ")"
    ' "$PLAN_JSON"
    echo "</details>"
  fi
}

if [[ "$PROTECTED_DESTROY_COUNT" -gt 0 && "$APPROVE_DESTROY" != "1" ]]; then
  echo "::error::Plan destroys $PROTECTED_DESTROY_COUNT protected resource(s) without iac:approve-destroy label" >&2
  exit 1
fi
exit 0
