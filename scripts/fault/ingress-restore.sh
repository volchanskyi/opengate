#!/usr/bin/env bash
# Revert a staging-only edge fault applied by ingress-apply.sh (FI4). Refuses any
# namespace but opengate-staging and is idempotent: with no saved state it is a
# no-op, so it is safe to run from a cleanup `trap` or workflow `always()`.
# See deploy/fault/ingress/README.md.
set -euo pipefail

ALLOWED_NAMESPACE="opengate-staging"
NAMESPACE="${NAMESPACE:-$ALLOWED_NAMESPACE}"
RELEASE="${RELEASE:-opengate-staging}"
INGRESS="${INGRESS:-$RELEASE}"
DEPLOYMENT="${DEPLOYMENT:-$RELEASE-server}"
FAULT_STATE_DIR="${FAULT_STATE_DIR:-/tmp/opengate-fault-state}"

usage() {
  cat >&2 <<EOF
usage: ${0##*/} <scenario>
  scenarios:
    edge-504   restore the staging Ingress proxy-timeout annotations
    edge-502   scale the staging server Deployment back to its saved replica count
Runs only against the '$ALLOWED_NAMESPACE' namespace.
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

[ "$NAMESPACE" = "$ALLOWED_NAMESPACE" ] || die "refusing to restore faults outside the '$ALLOWED_NAMESPACE' namespace (got '$NAMESPACE')"
command -v kubectl >/dev/null 2>&1 || die "kubectl not found on PATH"
command -v jq >/dev/null 2>&1 || die "jq not found on PATH"

restore_edge_504() {
  local state_file="$FAULT_STATE_DIR/edge-504.json"
  if [ ! -f "$state_file" ]; then
    echo "${0##*/}: no saved state for edge-504 — nothing to restore"
    return 0
  fi
  # A merge patch of the saved values; a null value (key was absent originally,
  # e.g. the fault marker) deletes the key under RFC 7386.
  local patch
  patch="$(jq -c '{metadata: {annotations: .}}' "$state_file")"
  kubectl patch ingress "$INGRESS" -n "$NAMESPACE" --type merge --patch "$patch" >/dev/null
  rm -f "$state_file"
  echo "${0##*/}: restored ingress/$INGRESS annotations in $NAMESPACE"
}

restore_edge_502() {
  local state_file="$FAULT_STATE_DIR/edge-502.replicas"
  if [ ! -f "$state_file" ]; then
    echo "${0##*/}: no saved state for edge-502 — nothing to restore"
    return 0
  fi
  local replicas
  replicas="$(cat "$state_file")"
  [ -n "$replicas" ] || replicas=1
  kubectl scale deploy "$DEPLOYMENT" -n "$NAMESPACE" --replicas="$replicas" >/dev/null
  rm -f "$state_file"
  echo "${0##*/}: scaled deploy/$DEPLOYMENT back to $replicas in $NAMESPACE"
}

case "$scenario" in
  edge-504) restore_edge_504 ;;
  edge-502) restore_edge_502 ;;
esac
