#!/usr/bin/env bash
# Wait for a pod to become ready, and say why if it never does.
#
# `kubectl wait` answers a pod that never became ready with "timed out waiting
# for the condition" and nothing else. The reason is sitting in the cluster the
# whole time, and the run is about to tear that cluster down.
#
# The nightly network drill lost a night to it. Its fleet pod asked for a tenth
# of a processor on a node with less than that left unclaimed, so the scheduler
# refused it and said so — "0/1 nodes are available: 1 Insufficient cpu" — and
# what reached the log was a timeout, then, one step later, a message about a pod
# that would not say whether it held a fleet. Neither names a pod that was never
# placed, and the difference matters: a pod that was never placed has certainly
# not started anything.
#
# So the wait reads the pod's own account before it gives up.
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

# The phase on its own is the headline: Pending is the scheduler, and anything
# else is the kubelet or the container. It is read separately from the account
# below so the one line that names the class of problem survives however long
# the rest is.
phase="$(kubectl -n "$NAMESPACE" get "pod/$POD" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
echo "::error::$POD was not ready inside ${TIMEOUT}s; it is ${phase:-not there at all}. What the cluster says about it follows." >&2

# describe carries the container statuses, what the pod asked for, and the
# scheduler's and kubelet's own events about it — which is where the reason is.
kubectl -n "$NAMESPACE" describe "pod/$POD" >&2 2>&1 || true

exit 1
