#!/usr/bin/env bash
# Waits for a pod to become ready and prints the cluster's account of it when it never does.
#
# Environment:
#   NAMESPACE   the namespace the pod is in (required)
#
# Usage:
#   NAMESPACE=… deploy/scripts/wait-for-pod-ready.sh <pod> [timeout-seconds]
set -euo pipefail

: "${NAMESPACE:?NAMESPACE is required}"

POD="${1:?a pod name is required}"
TIMEOUT="${2:-120}"

if kubectl -n "$NAMESPACE" wait --for=condition=Ready "pod/$POD" --timeout="${TIMEOUT}s"; then
  exit 0
fi

# The phase names the class of problem: Pending is the scheduler, anything else the kubelet.
phase="$(kubectl -n "$NAMESPACE" get "pod/$POD" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
echo "::error::$POD was not ready inside ${TIMEOUT}s; it is ${phase:-not there at all}. What the cluster says about it follows." >&2

# describe carries the container statuses and the scheduler and kubelet events.
kubectl -n "$NAMESPACE" describe "pod/$POD" >&2 2>&1 || true

exit 1
