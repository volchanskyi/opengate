#!/usr/bin/env bash
# Tests deploy/scripts/ingress-controller-apply.sh with helm and kubectl stubbed on PATH, then holds
# the values file and the opengate chart to what the controller is meant to run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APPLY="$REPO_ROOT/deploy/scripts/ingress-controller-apply.sh"
VALUES="$REPO_ROOT/deploy/ingress-nginx/values.yaml"
CHART="$REPO_ROOT/deploy/helm/opengate"
RENDERED="$SCRIPT_DIR/fixtures/ingress-nginx/controller-configmap.yaml"
[ -x "$APPLY" ] || {
  echo "FAIL: $APPLY not executable" >&2
  exit 1
}
# shellcheck source=scripts/lib/tool-versions.sh
. "$REPO_ROOT/scripts/lib/tool-versions.sh"
PINNED_CHART="ingress-nginx-$TOOL_VERSION_INGRESS_NGINX_CHART"

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
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
contains() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}
lacks() {
  if grep -qF -- "$2" <<<"$3"; then fail "$1 (found [$2] in [$3])"; else pass "$1"; fi
}

REAL_HELM="$(command -v helm || true)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
STUB="$WORK/stub"
mkdir -p "$STUB"
export STATE="$WORK/state" STUB_LOG="$WORK/calls.log"

# helm: an upgrade is recorded; a render prints the recorded chart render; a list prints releases.
cat >"$STUB/helm" <<'STUB_HELM'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >>"$STUB_LOG"
case "$1" in
  upgrade) exit 0 ;;
  template) cat "$STATE/render.yaml" ;;
  list) cat "$STATE/releases.json" ;;
  *) exit 1 ;;
esac
STUB_HELM
# kubectl: the Service and the ConfigMap the cluster holds, each absent when its file is.
cat >"$STUB/kubectl" <<'STUB_KUBECTL'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >>"$STUB_LOG"
args=" $* "
case "$args" in
  *" service ingress-nginx-controller "*)
    [ -f "$STATE/service.json" ] && cat "$STATE/service.json"
    exit 0
    ;;
  *" configmap ingress-nginx-controller "*)
    if [ -f "$STATE/configmap.json" ]; then cat "$STATE/configmap.json"; exit 0; fi
    echo 'Error from server (NotFound): configmaps "ingress-nginx-controller" not found' >&2
    exit 1
    ;;
esac
exit 1
STUB_KUBECTL
chmod +x "$STUB/helm" "$STUB/kubectl"

SUBNET='ocid1.subnet.oc1.us-sanjose-1.aaaaaaaaexamplesubnetforthetest'
OTHER_SUBNET='ocid1.subnet.oc1.us-sanjose-1.aaaaaaaaanothersubnetelsewhere'

# The cluster as a correct apply leaves it: the chart's ConfigMap, the declared annotations plus
# the subnet, and the pinned chart.
healthy() {
  rm -rf "$STATE"
  mkdir -p "$STATE"
  : >"$STUB_LOG"
  cp "$RENDERED" "$STATE/render.yaml"
  python3 - "$RENDERED" "$VALUES" "$SUBNET" "$STATE" "$PINNED_CHART" <<'PY'
import json
import sys

import yaml

rendered, values, subnet, state, chart = sys.argv[1:]
with open(rendered, encoding="utf-8") as handle:
    data = yaml.safe_load(handle)["data"] or {}
with open(values, encoding="utf-8") as handle:
    annotations = dict(yaml.safe_load(handle)["controller"]["service"]["annotations"])
annotations["service.beta.kubernetes.io/oci-load-balancer-subnet1"] = subnet
annotations["meta.helm.sh/release-name"] = "ingress-nginx"
with open(f"{state}/configmap.json", "w", encoding="utf-8") as handle:
    json.dump({"kind": "ConfigMap", "data": data}, handle)
with open(f"{state}/service.json", "w", encoding="utf-8") as handle:
    json.dump({"kind": "Service", "metadata": {"annotations": annotations}}, handle)
with open(f"{state}/releases.json", "w", encoding="utf-8") as handle:
    json.dump([{"name": "ingress-nginx", "chart": chart}], handle)
PY
}

# Rewrites one field of a state file through a Python expression over `doc`.
edit_state() {
  python3 - "$STATE/$1" "$2" <<'PY'
import json
import sys

path, change = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as handle:
    doc = json.load(handle)
exec(change)
with open(path, "w", encoding="utf-8") as handle:
    json.dump(doc, handle)
PY
}

run() {
  rc=0
  out="$(PATH="$STUB:$PATH" "$APPLY" "$@" 2>&1)" || rc=$?
}

echo "▶ apply on the running cluster"
healthy
run apply
assert_eq "apply exits 0 when the read-back agrees" 0 "$rc"
upgrade="$(grep '^helm upgrade ' "$STUB_LOG" || true)"
contains "apply installs the release" "upgrade --install ingress-nginx ingress-nginx" "$upgrade"
contains "apply installs the pinned chart version" "--version $TOOL_VERSION_INGRESS_NGINX_CHART" "$upgrade"
contains "apply drops values given earlier" "--reset-values" "$upgrade"
contains "apply takes the repository's values" "-f $VALUES" "$upgrade"
contains "apply keeps the running load balancer's subnet" \
  "controller.service.annotations.service\\.beta\\.kubernetes\\.io/oci-load-balancer-subnet1=$SUBNET" "$upgrade"

echo "▶ check reads back and installs nothing"
healthy
run check
assert_eq "check exits 0 when the cluster agrees" 0 "$rc"
lacks "check never installs" "helm upgrade" "$(cat "$STUB_LOG")"

echo "▶ the read-back names what differs"
healthy
edit_state configmap.json 'doc["data"] = {"allow-snippet-annotations": "true", "annotations-risk-level": "Critical"}'
run check
assert_eq "a ConfigMap allowing snippets fails" 1 "$rc"
contains "the failure says snippets are allowed" "allows snippet annotations" "$out"
contains "the failure names the key the chart does not render" "annotations-risk-level" "$out"

healthy
edit_state configmap.json 'doc["data"]["add-headers"] = "opengate/opengate-custom-headers"'
run check
assert_eq "a key the chart does not render fails" 1 "$rc"
contains "the failure names that key" "add-headers" "$out"

healthy
edit_state service.json 'doc["metadata"]["annotations"]["service.beta.kubernetes.io/oci-load-balancer-shape-flex-max"] = "100"'
run check
assert_eq "a load-balancer annotation that differs fails" 1 "$rc"
contains "the failure names the annotation" "oci-load-balancer-shape-flex-max" "$out"

healthy
edit_state releases.json 'doc[0]["chart"] = "ingress-nginx-4.12.0"'
run check
assert_eq "another chart version fails" 1 "$rc"
contains "the failure names the running chart" "ingress-nginx-4.12.0" "$out"

healthy
rm "$STATE/configmap.json"
run check
assert_eq "a missing ConfigMap fails" 1 "$rc"

echo "▶ the subnet is Terraform's"
healthy
INGRESS_LB_SUBNET_ID="$OTHER_SUBNET" run apply
assert_eq "a Service in another subnet than the one named is refused" 1 "$rc"
contains "the refusal names both subnets" "$OTHER_SUBNET" "$out"
lacks "a refused subnet installs nothing" "helm upgrade" "$(cat "$STUB_LOG")"

healthy
rm "$STATE/service.json"
run apply
assert_eq "apply with neither a Service nor a subnet is refused" 1 "$rc"
contains "the refusal names the setting" "INGRESS_LB_SUBNET_ID" "$out"
lacks "a refusal installs nothing" "helm upgrade" "$(cat "$STUB_LOG")"

healthy
rm "$STATE/service.json"
INGRESS_LB_SUBNET_ID="$SUBNET" run apply
upgrade="$(grep '^helm upgrade ' "$STUB_LOG" || true)"
contains "a new cluster takes the subnet named" "oci-load-balancer-subnet1=$SUBNET" "$upgrade"

healthy
rm "$STATE/service.json"
INGRESS_LB_SUBNET_ID='ocid1.vcn.oc1.us-sanjose-1.aaaaaaaanotasubnet' run apply
assert_eq "a value that is not a subnet id is refused" 1 "$rc"
lacks "a bad subnet installs nothing" "helm upgrade" "$(cat "$STUB_LOG")"

echo "▶ usage"
healthy
run
assert_eq "no mode is a usage error" 2 "$rc"

echo "▶ the values file"
values_report="$(
  python3 - "$VALUES" <<'PY'
import sys

import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    controller = yaml.safe_load(handle)["controller"]
config = controller.get("config") or {}
annotations = (controller.get("service") or {}).get("annotations") or {}
print(f"chart-snippets={controller.get('allowSnippetAnnotations')}")
print(f"config-snippets={config.get('allow-snippet-annotations')}")
print(f"risk-level={'annotations-risk-level' in config}")
print(f"subnet={any('subnet' in key for key in annotations)}")
PY
)"
contains "the chart is told snippets are off" "chart-snippets=False" "$values_report"
contains "the controller's own setting says snippets are off" "config-snippets=false" "$values_report"
contains "no risk level is overridden" "risk-level=False" "$values_report"
contains "the values name no subnet" "subnet=False" "$values_report"

echo "▶ the opengate chart"
if [ -z "$REAL_HELM" ]; then
  fail "helm is not installed, so the chart cannot be rendered"
else
  for overlay in values-production.yaml values-staging.yaml; do
    render="$("$REAL_HELM" template opengate "$CHART" -f "$CHART/$overlay" 2>&1)"
    contains "the chart renders with $overlay" "kind: Ingress" "$render"
    lacks "the chart renders no headers ConfigMap with $overlay" "custom-headers" "$render"
    lacks "the Ingress carries no snippet with $overlay" "snippet" "$render"
  done
fi

echo
echo "ingress-controller: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
