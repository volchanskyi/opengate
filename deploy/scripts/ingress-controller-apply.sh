#!/usr/bin/env bash
# Gives the cluster's ingress controller the pinned chart with deploy/ingress-nginx/values.yaml,
# then reads back what it holds; `check` reads back and installs nothing.
#
# Usage: ingress-controller-apply.sh apply|check
#
# Environment:
#   INGRESS_LB_SUBNET_ID  the load balancer's subnet, read when no controller Service runs yet
#
# Exit codes:
#   0  the cluster holds what the repository declares
#   1  it holds something else, or the subnet could not be settled
#   2  usage
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=scripts/lib/tool-versions.sh
. "$REPO_ROOT/scripts/lib/tool-versions.sh"

NAMESPACE=ingress-nginx
RELEASE=ingress-nginx
CHART_REPO=https://kubernetes.github.io/ingress-nginx
VALUES="$REPO_ROOT/deploy/ingress-nginx/values.yaml"
SUBNET_KEY=service.beta.kubernetes.io/oci-load-balancer-subnet1
SUBNET_ID='^ocid1\.subnet\.oc1\.[a-z0-9-]+\.[a-z0-9]+$'

refuse() {
  echo "::error::$1" >&2
  exit 1
}

mode="${1:-}"
case "$mode" in
  apply | check) ;;
  *)
    echo "usage: $0 apply|check" >&2
    exit 2
    ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

service() {
  kubectl -n "$NAMESPACE" get service "$RELEASE-controller" --ignore-not-found -o json >"$WORK/service.json"
}

# Terraform owns the subnet, and moving the load balancer to another one changes its address.
service
running_subnet="$(python3 -c '
import json, sys
text = open(sys.argv[1], encoding="utf-8").read()
print(((json.loads(text) if text.strip() else {}).get("metadata") or {}).get("annotations", {}).get(sys.argv[2], ""))
' "$WORK/service.json" "$SUBNET_KEY")"
named_subnet="${INGRESS_LB_SUBNET_ID:-}"
if [ -n "$running_subnet" ] && [ -n "$named_subnet" ] && [ "$running_subnet" != "$named_subnet" ]; then
  refuse "the running load balancer sits in $running_subnet and INGRESS_LB_SUBNET_ID names $named_subnet; moving it would change its address"
fi
subnet="${running_subnet:-$named_subnet}"
[ -n "$subnet" ] || refuse "no controller Service runs and INGRESS_LB_SUBNET_ID is unset, so the load balancer has no subnet to sit in"
[[ "$subnet" =~ $SUBNET_ID ]] || refuse "$subnet is not a subnet id"

chart=(
  "$RELEASE" ingress-nginx
  --repo "$CHART_REPO"
  --version "$TOOL_VERSION_INGRESS_NGINX_CHART"
  --namespace "$NAMESPACE"
  -f "$VALUES"
  --set-string "controller.service.annotations.${SUBNET_KEY//./\\.}=$subnet"
)

if [ "$mode" = apply ]; then
  helm upgrade --install "${chart[@]}" --create-namespace --reset-values --wait --timeout 5m
  service
fi

helm template "${chart[@]}" --show-only templates/controller-configmap.yaml >"$WORK/rendered.yaml"
kubectl -n "$NAMESPACE" get configmap "$RELEASE-controller" -o json >"$WORK/configmap.json" \
  || refuse "the controller ConfigMap could not be read"
helm list --namespace "$NAMESPACE" --filter "^$RELEASE\$" -o json >"$WORK/releases.json"

if ! python3 "$SCRIPT_DIR/ingress-controller-readback.py" "$WORK/rendered.yaml" "$WORK/configmap.json" \
  "$VALUES" "$SUBNET_KEY" "$subnet" "$WORK/service.json" "$WORK/releases.json" \
  "ingress-nginx-$TOOL_VERSION_INGRESS_NGINX_CHART" >"$WORK/differences.txt"; then
  while IFS= read -r difference; do echo "::error::$difference" >&2; done <"$WORK/differences.txt"
  refuse "the ingress controller holds something other than deploy/ingress-nginx/values.yaml at chart $TOOL_VERSION_INGRESS_NGINX_CHART"
fi
echo "the ingress controller holds deploy/ingress-nginx/values.yaml at chart $TOOL_VERSION_INGRESS_NGINX_CHART"
