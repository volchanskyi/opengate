#!/usr/bin/env bash
# Tests for deploy/scripts/authority-readers-check.sh: the deploy passes only when the cluster rule
# exists, refuses every outside pod by name, and admits the server's own pod.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/deploy/scripts/authority-readers-check.sh"
[ -x "$CHECK" ] || {
  echo "FAIL: $CHECK not executable" >&2
  exit 1
}

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
assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then pass "$name"; else fail "$name (missing [$needle])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

cat >"$WORK/deployment.json" <<'DEPLOYMENT'
{"spec": {"template": {"metadata": {"labels": {"app": "opengate-server"}}, "spec": {
  "serviceAccountName": "opengate",
  "securityContext": {"runAsNonRoot": true, "runAsUser": 65532},
  "initContainers": [{"name": "authority-stage", "image": "ghcr.io/volchanskyi/opengate-server:sha-abc",
    "volumeMounts": [{"name": "authority-secret", "mountPath": "/authority-secret"}]}],
  "containers": [{"name": "server", "image": "ghcr.io/volchanskyi/opengate-server:sha-abc"}],
  "volumes": [{"name": "authority-secret", "secret": {"secretName": "opengate-authority"}}]
}}}}
DEPLOYMENT

cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl: answers the rule's reads and judges each dry-run pod as FAKE_RULE says.
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_TRACE"
args=" $* "
case "$args" in
  *" get validatingadmissionpolicy opengate-authority-readers "*)
    [ "$FAKE_POLICY" = present ] && { echo "opengate-authority-readers"; exit 0; }
    echo 'Error from server (NotFound): validatingadmissionpolicies "opengate-authority-readers" not found' >&2
    exit 1
    ;;
  *" get validatingadmissionpolicybinding opengate-authority-readers "*)
    [ "$FAKE_BINDING" = present ] && { echo "opengate-authority-readers"; exit 0; }
    echo 'Error from server (NotFound): validatingadmissionpolicybindings "opengate-authority-readers" not found' >&2
    exit 1
    ;;
  *" get deploy opengate-server "*)
    cat "$FAKE_DEPLOYMENT"
    ;;
  *" create --dry-run=server "*)
    pod="$(cat)"
    printf '%s\n' "$pod" >>"$FAKE_PODS"
    verdict="$(python3 -c '
import json, sys
pod, rule = json.loads(sys.argv[1]), sys.argv[2]
spec = pod["spec"]
containers = spec.get("containers", []) + spec.get("initContainers", []) + spec.get("ephemeralContainers", [])
key = "opengate-authority"
reads = any(v.get("secret", {}).get("secretName") == key for v in spec.get("volumes", []))
reads |= any(s.get("secret", {}).get("name") == key
             for v in spec.get("volumes", []) for s in v.get("projected", {}).get("sources", []))
reads |= any(e.get("valueFrom", {}).get("secretKeyRef", {}).get("name") == key
             for c in containers for e in c.get("env", []))
reads |= any(f.get("secretRef", {}).get("name") == key for c in containers for f in c.get("envFrom", []))
server = spec.get("serviceAccountName") == "opengate" and all(
    c["image"].startswith("ghcr.io/volchanskyi/opengate-server:") for c in containers)
refused = {"working": reads and not server, "open": False, "closed": True, "other": True}[rule]
print("refused" if refused else "admitted")
' "$pod" "$FAKE_RULE")"
    if [ "$verdict" = admitted ]; then
      echo "pod/authority-check-x created (server dry run)"
      exit 0
    fi
    if [ "$FAKE_RULE" = other ]; then
      echo 'Error from server (Forbidden): pods "authority-check-x" is forbidden: violates PodSecurity "restricted:latest"' >&2
    else
      echo "Error from server (Forbidden): pods \"authority-check-x\" is forbidden: ValidatingAdmissionPolicy 'opengate-authority-readers' with binding 'opengate-authority-readers' denied request" >&2
    fi
    exit 1
    ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 64
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

# run executes the check with the stand-in on PATH; NAME=value words set the cluster's answers.
run() {
  : >"$WORK/trace"
  : >"$WORK/pods"
  env PATH="$WORK/bin:$PATH" FAKE_TRACE="$WORK/trace" FAKE_PODS="$WORK/pods" \
    FAKE_DEPLOYMENT="$WORK/deployment.json" FAKE_POLICY=present FAKE_BINDING=present FAKE_RULE=working \
    KUBECTL_RETRY_DELAY=0 "$@" bash "$CHECK"
}

echo "authority-readers-check:"

status=0
out="$(run 2>&1)" || status=$?
assert_eq "a working rule passes" "0" "$status"
assert_eq "every outside pod and the server's own pod are tried, and nothing is created" \
  "7 7" "$(grep -c -- '--dry-run=server' "$WORK/trace") $(grep -c . "$WORK/pods")"
assert_contains "the server's own pod is admitted" "admitted: the server's own pod" "$out"
for shape in "another account on the server's image" "the server's account on another image" \
  "a secret volume" "a projected volume" "a variable read from the secret" "every variable from the secret"; do
  assert_contains "refused by the rule: $shape" "refused by the rule: $shape" "$out"
done

status=0
out="$(run FAKE_POLICY=absent 2>&1)" || status=$?
assert_eq "a missing rule fails" "1" "$status"
assert_contains "a missing rule is named" "the cluster has no rule opengate-authority-readers" "$out"

status=0
out="$(run FAKE_BINDING=absent 2>&1)" || status=$?
assert_eq "a rule bound to nothing fails" "1" "$status"
assert_contains "an unbound rule is named" "the rule opengate-authority-readers is bound to nothing" "$out"

status=0
out="$(run FAKE_RULE=open 2>&1)" || status=$?
assert_eq "a rule that admits an outside pod fails" "1" "$status"
assert_contains "the admitted outside pod is named" "admitted, and the rule should refuse it: a secret volume" "$out"

status=0
out="$(run FAKE_RULE=closed 2>&1)" || status=$?
assert_eq "a rule that refuses the server fails" "1" "$status"
assert_contains "the refused server is named" "the server's own pod is refused" "$out"

status=0
out="$(run FAKE_RULE=other 2>&1)" || status=$?
assert_eq "a refusal from something other than the rule fails" "1" "$status"
assert_contains "a refusal from elsewhere proves nothing" "refused, but by something other than opengate-authority-readers" "$out"

echo
echo "authority-readers-check: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
