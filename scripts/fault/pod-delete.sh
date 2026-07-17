#!/usr/bin/env bash
# Single-pod deletion drill: delete the staging server pod by exact selector and
# assert the Deployment recovers a Ready replacement within the recovery SLO.
# Refuses any namespace but opengate-staging, captures evidence, and is
# idempotent (safe to re-run). Scheduled/manual — see docs/Fault-Injection.md.
set -euo pipefail

ALLOWED_NAMESPACE="opengate-staging"
NAMESPACE="${NAMESPACE:-$ALLOWED_NAMESPACE}"
RELEASE="${RELEASE:-opengate-staging}"
DEPLOYMENT="${DEPLOYMENT:-$RELEASE-server}"
SELECTOR="${SELECTOR:-app.kubernetes.io/instance=$RELEASE,app.kubernetes.io/component=server}"
SLO_SECONDS="${SLO_SECONDS:-120}"
EVIDENCE_DIR="${EVIDENCE_DIR:-${FAULT_STATE_DIR:-/tmp/opengate-fault-state}/pod-delete}"

die() {
  echo "pod-delete: $1" >&2
  exit "${2:-1}"
}

[ "$NAMESPACE" = "$ALLOWED_NAMESPACE" ] || die "refusing to run infra faults outside the '$ALLOWED_NAMESPACE' namespace (got '$NAMESPACE')"
command -v kubectl >/dev/null 2>&1 || die "kubectl not found on PATH"

mkdir -p "$EVIDENCE_DIR"

capture() {
  # Best-effort evidence; never fail the drill on a capture error.
  kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp >>"$EVIDENCE_DIR/pod-delete-events.txt" 2>&1 || true
  kubectl get pods -n "$NAMESPACE" -l "$SELECTOR" -o wide >>"$EVIDENCE_DIR/pod-delete-pods.txt" 2>&1 || true
}

trap capture EXIT

echo "pod-delete: deleting server pod(s) [$SELECTOR] in $NAMESPACE"
capture
kubectl delete pod -n "$NAMESPACE" -l "$SELECTOR" --wait=false

echo "pod-delete: waiting up to ${SLO_SECONDS}s for a Ready replacement (SLO)"
if ! kubectl rollout status "deploy/$DEPLOYMENT" -n "$NAMESPACE" --timeout="${SLO_SECONDS}s" >>"$EVIDENCE_DIR/pod-delete-rollout.txt" 2>&1; then
  die "recovery exceeded the ${SLO_SECONDS}s SLO — see $EVIDENCE_DIR/pod-delete-rollout.txt"
fi

echo "pod-delete: replacement Ready within the ${SLO_SECONDS}s SLO in $NAMESPACE"
