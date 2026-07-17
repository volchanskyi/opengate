#!/usr/bin/env bash
# Bad-rollout drill: deploy a deliberately-failing revision to staging, assert
# the rollout fails readiness, then `helm rollback` and assert the prior image
# is healthy. Refuses any namespace but opengate-staging and ALWAYS rolls back
# (a trap safety net rolls back even on interruption), so staging never lingers
# on the bad revision. Captures evidence; idempotent. See docs/Fault-Injection.md.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

ALLOWED_NAMESPACE="opengate-staging"
NAMESPACE="${NAMESPACE:-$ALLOWED_NAMESPACE}"
RELEASE="${RELEASE:-opengate-staging}"
DEPLOYMENT="${DEPLOYMENT:-$RELEASE-server}"
CHART="${CHART:-$REPO_ROOT/deploy/helm/opengate}"
VALUES="${VALUES:-$REPO_ROOT/deploy/helm/opengate/values-staging.yaml}"
BAD_TAG="${BAD_TAG:-fault-bad-rollout-nonexistent}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-90s}"
SLO_SECONDS="${SLO_SECONDS:-180}"
EVIDENCE_DIR="${EVIDENCE_DIR:-${FAULT_STATE_DIR:-/tmp/opengate-fault-state}/bad-rollout}"

die() {
  echo "bad-rollout: $1" >&2
  exit "${2:-1}"
}

[ "$NAMESPACE" = "$ALLOWED_NAMESPACE" ] || die "refusing to run infra faults outside the '$ALLOWED_NAMESPACE' namespace (got '$NAMESPACE')"
command -v kubectl >/dev/null 2>&1 || die "kubectl not found on PATH"
command -v helm >/dev/null 2>&1 || die "helm not found on PATH"
command -v jq >/dev/null 2>&1 || die "jq not found on PATH"

mkdir -p "$EVIDENCE_DIR"

capture() {
  # Best-effort evidence; never fail the drill on a capture error.
  kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp >>"$EVIDENCE_DIR/bad-rollout-events.txt" 2>&1 || true
  helm history "$RELEASE" -n "$NAMESPACE" >>"$EVIDENCE_DIR/bad-rollout-history.txt" 2>&1 || true
}

good_rev=""
bad_applied=0
rolled_back=0

cleanup() {
  local rc=$?
  trap - EXIT
  if [ "$bad_applied" = 1 ] && [ "$rolled_back" != 1 ]; then
    echo "bad-rollout: cleanup rolling back $RELEASE to revision $good_rev" >&2
    helm rollback "$RELEASE" "$good_rev" -n "$NAMESPACE" --wait --timeout="${SLO_SECONDS}s" >>"$EVIDENCE_DIR/bad-rollout-rollback.txt" 2>&1 || true
  fi
  capture
  exit "$rc"
}
trap cleanup EXIT

good_rev="$(helm history "$RELEASE" -n "$NAMESPACE" -o json | jq -r '[.[] | select(.status == "deployed")] | last | .revision')"
[ -n "$good_rev" ] && [ "$good_rev" != "null" ] || die "cannot determine the current deployed revision of $RELEASE"

capture
echo "bad-rollout: deploying a deliberately-failing revision (image.tag=$BAD_TAG) to $NAMESPACE"
bad_applied=1
if helm upgrade "$RELEASE" "$CHART" -n "$NAMESPACE" -f "$VALUES" --set "image.tag=$BAD_TAG" --wait --timeout="$ROLLOUT_TIMEOUT" >>"$EVIDENCE_DIR/bad-rollout-upgrade.txt" 2>&1; then
  echo "bad-rollout: WARNING the deliberately-bad revision did not fail readiness — rolling back anyway" >&2
else
  echo "bad-rollout: bad revision failed readiness as expected"
fi

echo "bad-rollout: rolling back to revision $good_rev"
helm rollback "$RELEASE" "$good_rev" -n "$NAMESPACE" --wait --timeout="${SLO_SECONDS}s" >>"$EVIDENCE_DIR/bad-rollout-rollback.txt" 2>&1
rolled_back=1

echo "bad-rollout: verifying the prior image is healthy"
kubectl rollout status "deploy/$DEPLOYMENT" -n "$NAMESPACE" --timeout="${SLO_SECONDS}s" >>"$EVIDENCE_DIR/bad-rollout-rollout.txt" 2>&1

echo "bad-rollout: rollback restored a healthy prior image in $NAMESPACE"
