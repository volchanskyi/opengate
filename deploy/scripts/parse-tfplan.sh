#!/usr/bin/env bash
# Parse `terraform show -json` output, emit a human-readable summary suitable
# for a PR comment, and fail (exit 1) if a destroy action targets a protected
# resource type — unless the caller asserts the destroy has been approved.
#
# Invoked by .github/workflows/iac-plan-preview.yml (S4 of the IaC pyramid).
#
# Usage:
#   parse-tfplan.sh <tfplan.json> [--approve-destroy]
#
# Output (stdout): markdown summary, e.g.
#     **Resource changes:** 1 to add, 2 to change, 0 to destroy.
#     <details><summary>Per-resource actions</summary>
#     - + module.compute.oci_core_instance.opengate
#     ...
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

# Resources whose destruction has high blast radius (data loss, networking-level
# outage, or tfstate loss). Adding to this list is a security decision — the
# protected set should grow over time, never shrink.
PROTECTED_TYPES=(
  oci_core_vcn
  oci_core_subnet
  oci_core_security_list
  oci_core_network_security_group
  oci_objectstorage_bucket
)

# Build a jq array of protected types
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

# Emit markdown summary to stdout (consumed by the workflow as the PR body).
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

# Final gate decision.
if [[ "$PROTECTED_DESTROY_COUNT" -gt 0 && "$APPROVE_DESTROY" != "1" ]]; then
  echo "::error::Plan destroys $PROTECTED_DESTROY_COUNT protected resource(s) without iac:approve-destroy label" >&2
  exit 1
fi
exit 0
