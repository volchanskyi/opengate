#!/usr/bin/env bash
# Tests that routine releases render nothing cluster-wide with their switches off, and that the
# cluster job's renderings carry the keep marker Helm honours when a release drops them.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OPENGATE="$REPO_ROOT/deploy/helm/opengate"
MONITORING="$REPO_ROOT/deploy/helm/monitoring"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

echo "cluster-scoped-split:"
command -v helm >/dev/null 2>&1 || {
  echo "FAIL: helm is not installed" >&2
  exit 1
}

# kinds prints each rendered document as KIND/NAME/KEEP, one per line, sorted.
kinds() {
  python3 -c '
import sys, yaml
for d in yaml.safe_load_all(sys.stdin):
    if d:
        keep = (d["metadata"].get("annotations") or {}).get("helm.sh/resource-policy", "-")
        print("/".join([d["kind"], d["metadata"]["name"], keep]))
' | sort
}
CLUSTER_KINDS='^(ClusterRole|ClusterRoleBinding|ClusterIssuer|Namespace|ValidatingAdmissionPolicy)/'

for values in values-production values-staging; do
  routine="$(helm template og "$OPENGATE" -n ns -f "$OPENGATE/$values.yaml" --set image.tag=x \
    --set certManager.create=false | kinds)"
  assert_eq "the routine $values release renders nothing cluster-wide" \
    "" "$(grep -E "$CLUSTER_KINDS" <<<"$routine" || true)"
done
issuer="$(helm template og "$OPENGATE" -n ns -f "$OPENGATE/values-production.yaml" --set image.tag=x \
  --show-only templates/clusterissuer.yaml | kinds)"
assert_eq "the cluster job's issuer keeps when a release stops rendering it" \
  "ClusterIssuer/letsencrypt-prod/keep" "$issuer"

routine="$(helm template monitoring "$MONITORING" -n monitoring -f "$MONITORING/values-production.yaml" \
  --set domain=example.test --set clusterRBAC.create=false | kinds)"
assert_eq "the routine monitoring release renders nothing cluster-wide" \
  "" "$(grep -E "$CLUSTER_KINDS" <<<"$routine" || true)"
cluster="$(helm template monitoring "$MONITORING" -n monitoring -f "$MONITORING/values-production.yaml" \
  --set domain=example.test --show-only templates/cluster-rbac.yaml | kinds)"
assert_eq "the cluster job renders monitoring's cluster roles under their own names, kept" \
  "ClusterRole/monitoring-promtail/keep
ClusterRole/monitoring-victoriametrics/keep
ClusterRoleBinding/monitoring-promtail/keep
ClusterRoleBinding/monitoring-victoriametrics/keep" "$cluster"

echo
echo "cluster-scoped-split: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
