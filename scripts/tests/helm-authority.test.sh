#!/usr/bin/env bash
# Tests that the master key lives in its own secret, reaches the server's user alone through a
# start-up copy, and that the key tool's jobs reach that secret and nothing they do not need.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHART="$REPO_ROOT/deploy/helm/opengate"

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

echo "helm-authority:"
command -v helm >/dev/null 2>&1 || {
  echo "FAIL: helm is not installed, so the chart cannot be rendered" >&2
  exit 1
}

# render prints the chart's documents as one JSON array.
render() {
  local yaml
  yaml="$(helm template opengate "$CHART" --namespace opengate --set image.tag=v1.2.3 "$@")"
  python3 -c '
import json, sys, yaml
print(json.dumps([d for d in yaml.safe_load_all(sys.stdin) if d]))
' <<<"$yaml"
}

# q runs a jq filter over a rendering and prints its raw answer.
q() {
  jq -r "$2" <<<"$1"
}

PROD="$(render -f "$CHART/values-production.yaml")"
STAGING="$(render -f "$CHART/values-staging.yaml")"

deploy='.[] | select(.kind == "Deployment" and .metadata.name == "opengate-server") | .spec.template.spec'
server="$deploy | .containers[] | select(.name == \"server\")"

assert_eq "production's server refuses to start without the master key" \
  "1" "$(q "$PROD" "[$server | .args[] | select(. == \"-ca-must-exist\")] | length")"

assert_eq "the authority secret is mounted whole, owner-read only" \
  "opengate-authority 256 none" \
  "$(q "$PROD" "$deploy | .volumes[] | select(.name == \"authority-secret\") | .secret | \"\(.secretName) \(.defaultMode) \(.items // \"none\")\"")"

assert_eq "the start-up step copies the key into the server's folder" \
  "opengate-authority stage -from /authority-secret -to /data/authority" \
  "$(q "$PROD" "$deploy | .initContainers[] | select(.name == \"authority-stage\") | .command | join(\" \")")"

assert_eq "the server's folder is memory, never disk" \
  "Memory" "$(q "$PROD" "$deploy | .volumes[] | select(.name == \"authority\") | .emptyDir.medium")"

assert_eq "the server reads the copy, read-only" \
  "/data/authority true" \
  "$(q "$PROD" "$server | .volumeMounts[] | select(.name == \"authority\") | \"\(.mountPath) \(.readOnly)\"")"

assert_eq "the server never mounts the secret itself" \
  "0" "$(q "$PROD" "[$server | .volumeMounts[] | select(.name == \"authority-secret\")] | length")"

assert_eq "the shared secret hands the server no master key" \
  "0" "$(q "$PROD" "[$deploy | .volumes[] | .secret.items // [] | .[] | select(.key | startswith(\"ca\"))] | length")"

staging_stage="$(q "$STAGING" "[$deploy | .initContainers // [] | .[] | select(.name == \"authority-stage\")] | length")"
staging_must="$(q "$STAGING" "[$server | .args[] | select(. == \"-ca-must-exist\")] | length")"
assert_eq "staging keeps a master key of its own, generated on start" "0 0" "$staging_stage $staging_must"

compromised="$(render -f "$CHART/values-production.yaml" --set authority.compromisedSince=2036-01-02T03:04:05Z)"
assert_eq "an emergency layout names when the old key leaked" \
  "2036-01-02T03:04:05Z" \
  "$(q "$compromised" "$server | .args as \$a | \$a | index(\"-ca-compromised-since\") as \$i | \$a[\$i + 1]")"

# A release holds no right over the master key; an administrator runs every key step by hand.
for rendering in PROD STAGING; do
  assert_eq "$rendering renders no key-tool job, account or role" \
    "0" "$(q "${!rendering}" "[.[] | select(.metadata.labels[\"app.kubernetes.io/component\"] == \"authority\")] | length")"
  assert_eq "no role in $rendering names the authority secret" \
    "0" "$(q "${!rendering}" "[.[] | select(.kind == \"Role\") | .rules[] | .resourceNames // [] | .[] | select(. == \"opengate-authority\")] | length")"
done

# The one-time move out of the shared secret, run as the tool job's adopt command.
ADOPT="$(render -f "$CHART/values-production.yaml" --show-only templates/authority-tool-job.yaml \
  --set authority.job.enabled=true --set-json 'authority.job.args=["adopt"]')"
adopt_job='.[] | select(.kind == "Job")'
assert_eq "adopt moves the shared secret's master key into the authority secret" \
  "adopt -secret opengate-authority -legacy-dir /legacy -legacy-secret opengate-secrets" \
  "$(q "$ADOPT" "$adopt_job | .spec.template.spec.containers[0].args | join(\" \")")"

assert_eq "adopt reads the shared secret's master key as optional files" \
  "opengate-secrets true ca.crt,ca.key" \
  "$(q "$ADOPT" "$adopt_job | .spec.template.spec.volumes[] | select(.name == \"legacy\") | .secret | \"\(.secretName) \(.optional) \([.items[].key] | join(\",\"))\"")"

assert_eq "adopt may only remove keys from the shared secret" \
  "patch" \
  "$(q "$ADOPT" "[.[] | select(.kind == \"Role\") | .rules[] | select((.resourceNames // []) | index(\"opengate-secrets\")) | .verbs[]] | join(\",\")")"

# The operator's tool job, rendered only by scripts/authority-job.sh.
TOOL="$(render -f "$CHART/values-production.yaml" --show-only templates/authority-tool-job.yaml \
  --set authority.job.enabled=true --set-json 'authority.job.args=["layout","-phase","changeover"]')"
tool_job='.[] | select(.kind == "Job")'
assert_eq "the tool job runs the command it was given, on the chart's secret" \
  "layout -secret opengate-authority -phase changeover" "$(q "$TOOL" "$tool_job | .spec.template.spec.containers[0].args | join(\" \")")"

assert_eq "the tool job mounts no secret" \
  "0" "$(q "$TOOL" "[$tool_job | .spec.template.spec.volumes // [] | .[] | select(.secret)] | length")"

assert_eq "the tool job reads and writes the authority secret and no other" \
  "opengate-authority" \
  "$(q "$TOOL" "[.[] | select(.kind == \"Role\") | .rules[] | .resourceNames // [] | .[]] | unique | join(\",\")")"

assert_eq "the tool job belongs to no release" \
  "0" "$(q "$TOOL" "[.[] | .metadata.annotations // {} | select(has(\"helm.sh/hook\"))] | length")"

for rendering in PROD TOOL ADOPT; do
  assert_eq "no role in $rendering lists or watches secrets" \
    "0" "$(q "${!rendering}" "[.[] | select(.kind == \"Role\") | .rules[] | select(.verbs | index(\"list\") or index(\"watch\"))] | length")"
done

DEFAULT_TOOL="$(helm template opengate "$CHART" --show-only templates/authority-tool-job.yaml 2>&1 || true)"
if grep -qF 'could not find template' <<<"$DEFAULT_TOOL"; then
  pass "a release renders no tool job"
else
  fail "a release must render no tool job: $DEFAULT_TOOL"
fi

echo
echo "helm-authority: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
