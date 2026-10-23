#!/usr/bin/env bash
# Gives the cluster what a routine deploy's login cannot write: namespaces, Helm records as
# configmaps, the master key's guard rule, workflow access, the issuer, cluster roles, ingress.
#
# Usage: deploy/scripts/cluster-apply.sh
#
# Environment:
#   WORKFLOW_USER_OCID  the workflows' cloud user that workflow-access.yaml binds (required)
#   ACME_EMAIL          the production issuer's ACME contact (required)
#   INGRESS_APPLY       the ingress controller step, default ingress-controller-apply.sh beside this
#   CLUSTER_RUN_REASON  why the deploy started this run, written at the top of the job summary
#   GITHUB_STEP_SUMMARY the job summary, which gets one row per piece and what the cluster did
#
# Exit codes:
#   0  every piece is applied
#   1  the cluster refused a piece
#   2  a required input is missing
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INGRESS_APPLY="${INGRESS_APPLY:-$ROOT/deploy/scripts/ingress-controller-apply.sh}"
# Every Helm call keeps its release record in a configmap, which the workflows' login can read.
export HELM_DRIVER=configmap

WORKFLOW_USER_OCID="${WORKFLOW_USER_OCID:-}"
ACME_EMAIL="${ACME_EMAIL:-}"
for input in WORKFLOW_USER_OCID ACME_EMAIL; do
  if [ -z "${!input}" ]; then
    echo "cluster-apply: $input is required" >&2
    exit 2
  fi
done

# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

# summarize LINE appends LINE to the job summary, when the run has one.
summarize() {
  [ -z "${GITHUB_STEP_SUMMARY:-}" ] || printf '%s\n' "$1" >>"$GITHUB_STEP_SUMMARY"
}

# record PIECE OUTPUT writes PIECE's summary row, each line of OUTPUT an entry in its cell.
record() {
  local cell="${2//|/\\|}"
  summarize "| ${1^} | ${cell//$'\n'/<br>} |"
}

# refuse PIECE REASON records PIECE as refused, names it in the log and stops the run.
refuse() {
  record "$1" "refused: $2"
  echo "cluster-apply: the cluster refused $1" >&2
  exit 1
}

# apply PIECE reads a manifest from stdin, applies it, and records what each object became.
apply() {
  local out
  if ! out="$(kubectl_retry --stdin apply -f - 2>&1)"; then
    printf '%s\n' "$out" >&2
    refuse "$1" "${out%%$'\n'*}"
  fi
  printf '%s\n' "$out"
  record "$1" "$out"
}

summarize "## Admin cluster changes"
summarize ""
summarize "**Why it ran:** ${CLUSTER_RUN_REASON:-not stated by the caller}"
summarize ""
summarize "| Piece | What the cluster did |"
summarize "|---|---|"

apply "the namespaces" <<'NAMESPACES'
apiVersion: v1
kind: Namespace
metadata:
  name: opengate
---
apiVersion: v1
kind: Namespace
metadata:
  name: opengate-staging
---
apiVersion: v1
kind: Namespace
metadata:
  name: monitoring
NAMESPACES

if ! migrated="$("$ROOT/deploy/scripts/helm-release-migrate.sh" opengate opengate-staging monitoring \
  ingress-nginx 2>&1)"; then
  printf '%s\n' "$migrated" >&2
  refuse "Helm's release records" "${migrated##*$'\n'}"
fi
printf '%s\n' "$migrated"
record "Helm's release records" "${migrated//helm-release-migrate: /}"

apply "the master key's guard rule" <"$ROOT/deploy/cluster/authority-readers.yaml"
apply "the workflows' access" \
  <<<"$(sed "s/WORKFLOW_USER_OCID/$WORKFLOW_USER_OCID/g" "$ROOT/deploy/cluster/workflow-access.yaml")"
apply "the production issuer" <<<"$(helm template opengate "$ROOT/deploy/helm/opengate" -n opengate \
  -f "$ROOT/deploy/helm/opengate/values-production.yaml" --set certManager.email="$ACME_EMAIL" \
  --show-only templates/clusterissuer.yaml)"
apply "monitoring's cluster roles" <<<"$(helm template monitoring "$ROOT/deploy/helm/monitoring" \
  -n monitoring -f "$ROOT/deploy/helm/monitoring/values-production.yaml" \
  --show-only templates/cluster-rbac.yaml)"

# The controller's upgrade streams to the log; its last line is what the cluster holds.
ingress_log="$(mktemp)"
trap 'rm -f "$ingress_log"' EXIT
"$INGRESS_APPLY" apply 2>&1 | tee "$ingress_log" || refuse "the ingress controller" "$(tail -n 1 "$ingress_log")"
record "the ingress controller" "$(tail -n 1 "$ingress_log")"
echo "cluster-apply: every cluster-wide piece is applied"
