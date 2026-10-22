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

# apply STEP reads a manifest from stdin and applies it, naming STEP when the cluster refuses it.
apply() {
  kubectl_retry --stdin apply -f - >/dev/null || {
    echo "cluster-apply: the cluster refused $1" >&2
    exit 1
  }
}

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

"$ROOT/deploy/scripts/helm-release-migrate.sh" opengate opengate-staging monitoring ingress-nginx

apply "the master key's guard rule" <"$ROOT/deploy/cluster/authority-readers.yaml"
apply "the workflows' access" \
  <<<"$(sed "s/WORKFLOW_USER_OCID/$WORKFLOW_USER_OCID/g" "$ROOT/deploy/cluster/workflow-access.yaml")"
apply "the production issuer" <<<"$(helm template opengate "$ROOT/deploy/helm/opengate" -n opengate \
  -f "$ROOT/deploy/helm/opengate/values-production.yaml" --set certManager.email="$ACME_EMAIL" \
  --show-only templates/clusterissuer.yaml)"
apply "monitoring's cluster roles" <<<"$(helm template monitoring "$ROOT/deploy/helm/monitoring" \
  -n monitoring -f "$ROOT/deploy/helm/monitoring/values-production.yaml" \
  --show-only templates/cluster-rbac.yaml)"

"$INGRESS_APPLY" apply
echo "cluster-apply: every cluster-wide piece is applied"
