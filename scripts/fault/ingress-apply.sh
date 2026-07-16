#!/usr/bin/env bash
# Apply a staging-only edge fault at ingress-nginx (FI4). Reverts with
# ingress-restore.sh. Refuses any namespace but opengate-staging, snapshots the
# fields it touches so the change is reversible, and restores best-effort if the
# apply fails partway. See deploy/fault/ingress/README.md.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

ALLOWED_NAMESPACE="opengate-staging"
NAMESPACE="${NAMESPACE:-$ALLOWED_NAMESPACE}"
RELEASE="${RELEASE:-opengate-staging}"
INGRESS="${INGRESS:-$RELEASE}"
DEPLOYMENT="${DEPLOYMENT:-$RELEASE-server}"
FAULT_STATE_DIR="${FAULT_STATE_DIR:-/tmp/opengate-fault-state}"
TEMPLATE_DIR="$REPO_ROOT/deploy/fault/ingress"
RESTORE_SCRIPT="$SCRIPT_DIR/ingress-restore.sh"

usage() {
  cat >&2 <<EOF
usage: ${0##*/} <scenario>
  scenarios:
    edge-504   shrink the staging Ingress proxy timeout so a backend delay -> 504
    edge-502   scale the staging server Deployment to zero so upstream -> 502
Runs only against the '$ALLOWED_NAMESPACE' namespace. Revert with ingress-restore.sh.
EOF
}

die() {
  echo "${0##*/}: $1" >&2
  exit "${2:-1}"
}

[ $# -eq 1 ] || {
  usage
  exit 2
}
scenario="$1"

case "$scenario" in
  edge-502 | edge-504) ;;
  *)
    echo "${0##*/}: unknown scenario '$scenario'" >&2
    usage
    exit 2
    ;;
esac

[ "$NAMESPACE" = "$ALLOWED_NAMESPACE" ] || die "refusing to apply faults outside the '$ALLOWED_NAMESPACE' namespace (got '$NAMESPACE')"
command -v kubectl >/dev/null 2>&1 || die "kubectl not found on PATH"
command -v jq >/dev/null 2>&1 || die "jq not found on PATH"

mkdir -p "$FAULT_STATE_DIR"

restore_on_error() {
  local rc=$?
  trap - ERR
  echo "${0##*/}: '$scenario' failed (rc=$rc) — attempting restore" >&2
  NAMESPACE="$NAMESPACE" RELEASE="$RELEASE" INGRESS="$INGRESS" DEPLOYMENT="$DEPLOYMENT" \
    FAULT_STATE_DIR="$FAULT_STATE_DIR" "$RESTORE_SCRIPT" "$scenario" || true
  exit "$rc"
}

apply_edge_504() {
  local state_file="$FAULT_STATE_DIR/edge-504.json"
  local orig
  orig="$(kubectl get ingress "$INGRESS" -n "$NAMESPACE" -o json | jq -c '.metadata.annotations // {}')"
  trap restore_on_error ERR
  # Snapshot the original value (or null, meaning absent) of every key the fault
  # overwrites, so restore can return the Ingress byte-identical.
  printf '%s' "$orig" | jq -c '{
    "nginx.ingress.kubernetes.io/proxy-read-timeout": .["nginx.ingress.kubernetes.io/proxy-read-timeout"],
    "nginx.ingress.kubernetes.io/proxy-send-timeout": .["nginx.ingress.kubernetes.io/proxy-send-timeout"],
    "fault.opengate.dev/scenario": .["fault.opengate.dev/scenario"]
  }' >"$state_file"
  kubectl patch ingress "$INGRESS" -n "$NAMESPACE" --type merge \
    --patch-file "$TEMPLATE_DIR/edge-504-timeout.json" >/dev/null
  echo "${0##*/}: applied edge-504 (proxy timeout -> 5s) on ingress/$INGRESS in $NAMESPACE"
}

apply_edge_502() {
  local state_file="$FAULT_STATE_DIR/edge-502.replicas"
  local replicas
  replicas="$(kubectl get deploy "$DEPLOYMENT" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}')"
  trap restore_on_error ERR
  printf '%s' "${replicas:-1}" >"$state_file"
  kubectl scale deploy "$DEPLOYMENT" -n "$NAMESPACE" --replicas=0 >/dev/null
  echo "${0##*/}: applied edge-502 (scaled deploy/$DEPLOYMENT to 0) in $NAMESPACE"
}

case "$scenario" in
  edge-504) apply_edge_504 ;;
  edge-502) apply_edge_502 ;;
esac

echo "${0##*/}: revert with 'NAMESPACE=$NAMESPACE $RESTORE_SCRIPT $scenario'"
