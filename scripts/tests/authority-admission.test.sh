#!/usr/bin/env bash
# Tests that the cluster rule guarding the master key names the chart's own secret, account, image
# and namespace, refuses every way a pod can read a secret, and denies on its own failure.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHART="$REPO_ROOT/deploy/helm/opengate"
POLICY_FILE="$REPO_ROOT/deploy/cluster/authority-readers.yaml"

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

echo "authority-admission:"
for tool in helm jq; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "FAIL: $tool is not installed" >&2
    exit 1
  }
done
[ -f "$POLICY_FILE" ] || {
  echo "FAIL: $POLICY_FILE is missing" >&2
  exit 1
}

# to_json prints a YAML stream's documents as one JSON array.
to_json() {
  python3 -c '
import json, sys, yaml
print(json.dumps([d for d in yaml.safe_load_all(sys.stdin) if d]))
'
}

RULES="$(to_json <"$POLICY_FILE")"
PROD="$(helm template opengate "$CHART" --namespace opengate --set image.tag=v1.2.3 \
  -f "$CHART/values-production.yaml" | to_json)"

policy='.[] | select(.kind == "ValidatingAdmissionPolicy")'
binding='.[] | select(.kind == "ValidatingAdmissionPolicyBinding")'
expressions="$(jq -r "[$policy | .spec.variables[].expression, .spec.validations[].expression] | join(\"\n\")" <<<"$RULES")"

secret="$(jq -r '.authority.secretName' <<<"$(helm show values "$CHART" | to_json | jq '.[0]')")"
repository="$(jq -r '.image.repository' <<<"$(helm show values "$CHART" | to_json | jq '.[0]')")"
account="$(jq -r '.[] | select(.kind == "Deployment" and .metadata.name == "opengate-server") | .spec.template.spec.serviceAccountName' <<<"$PROD")"
namespace="$(sed -nE 's/^NAMESPACE="\$\{NAMESPACE:-([a-z0-9-]+)\}"$/\1/p' "$REPO_ROOT/scripts/authority-job.sh")"

assert_eq "the rule guards the chart's authority secret" \
  "1" "$(grep -cF "'$secret'" <<<"$expressions")"
assert_eq "the rule admits the server's own account" \
  "1" "$(grep -cF "serviceAccountName == '$account'" <<<"$expressions")"
images="$(grep -oE "startsWith\('${repository}[:@]'\)" <<<"$expressions" || true)"
assert_eq "the rule admits only the server's image, by tag or by digest" \
  "2" "$(grep -c . <<<"$images")"
assert_eq "the binding applies the rule in the release's namespace" \
  "$namespace" "$(jq -r "$binding | .spec.matchResources.namespaceSelector.matchLabels[\"kubernetes.io/metadata.name\"]" <<<"$RULES")"
assert_eq "the binding names the rule and denies" \
  "opengate-authority-readers Deny" \
  "$(jq -r "$binding | \"\(.spec.policyName) \(.spec.validationActions | join(\",\"))\"" <<<"$RULES")"
assert_eq "the rule refuses when it cannot decide" \
  "Fail" "$(jq -r "$policy | .spec.failurePolicy" <<<"$RULES")"
assert_eq "the rule reads every new or changed pod, debug containers included" \
  "CREATE,UPDATE pods,pods/ephemeralcontainers" \
  "$(jq -r "$policy | .spec.matchConstraints.resourceRules[0] | \"\(.operations | join(\",\")) \(.resources | join(\",\"))\"" <<<"$RULES")"

for way in "v.secret.secretName" "s.secret.name" "e.valueFrom.secretKeyRef.name" "f.secretRef.name" \
  "initContainers" "ephemeralContainers"; do
  if grep -qF -- "$way" <<<"$expressions"; then
    pass "the rule reads $way"
  else
    fail "the rule never reads $way"
  fi
done

echo
echo "authority-admission: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
