#!/usr/bin/env bash
# Tests for deploy/scripts/cluster-apply.sh: the cluster job applies each piece a routine login
# cannot write, in order, with the workflows' user filled in, and refuses to run without its inputs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APPLY="$REPO_ROOT/deploy/scripts/cluster-apply.sh"
[ -x "$APPLY" ] || {
  echo "FAIL: $APPLY not executable" >&2
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/applied"

cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl: keeps each applied document set in order and answers an empty secret list.
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_TRACE"
args=" $* "
case "$args" in
  *" apply -f - "*)
    n="$(find "$FAKE_APPLIED" -type f | wc -l | tr -d ' ')"
    cat >"$FAKE_APPLIED/$(printf '%02d' "$n").yaml"
    if [ "$n" = "${FAKE_REFUSE_AT:-}" ]; then
      echo "Error from server (Forbidden): refused set $n" >&2
      exit 1
    fi
    printf 'set%s/first unchanged\nset%s/second configured\n' "$n" "$n"
    ;;
  *" get secrets -l owner=helm "*) echo '{"items": []}' ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 64
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

cat >"$WORK/ingress-apply" <<'FAKE_INGRESS'
#!/usr/bin/env bash
printf 'ingress %s HELM_DRIVER=%s\n' "$*" "${HELM_DRIVER:-}" >>"$FAKE_TRACE"
echo "Release \"ingress-nginx\" has been upgraded."
echo "the ingress controller holds deploy/ingress-nginx/values.yaml at chart 9.9.9"
FAKE_INGRESS
chmod +x "$WORK/ingress-apply"

run() {
  : >"$WORK/trace"
  rm -f "$WORK"/applied/* "$WORK/summary"
  env PATH="$WORK/bin:$PATH" FAKE_TRACE="$WORK/trace" FAKE_APPLIED="$WORK/applied" \
    KUBECTL_RETRY_DELAY=0 INGRESS_APPLY="$WORK/ingress-apply" HELM_DRIVER=configmap \
    GITHUB_STEP_SUMMARY="$WORK/summary" CLUSTER_RUN_REASON="changed since abc1234: deploy/cluster" \
    WORKFLOW_USER_OCID="ocid1.user.oc1..workflow" ACME_EMAIL="ops@example.test" "$@" bash "$APPLY"
}

# kinds_in prints KIND/NAME for every document in one applied set.
kinds_in() {
  python3 -c '
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if d:
        print(d["kind"] + "/" + d["metadata"]["name"])
' "$1" | sort | tr '\n' ' '
}

echo "cluster-apply:"

status=0
run >"$WORK/out" 2>&1 || status=$?
assert_eq "a full run succeeds" "0" "$status"
assert_eq "the namespaces come first" \
  "Namespace/monitoring Namespace/opengate Namespace/opengate-staging " "$(kinds_in "$WORK/applied/00.yaml")"
assert_eq "Helm's records move into configmaps in every release namespace before anything reads them" \
  "opengate opengate-staging monitoring ingress-nginx" \
  "$(sed -nE 's/^-n ([a-z-]+) get secrets -l owner=helm.*/\1/p' "$WORK/trace" | tr '\n' ' ' | sed 's/ $//')"
assert_eq "the master key's guard rule is applied" \
  "ValidatingAdmissionPolicy/opengate-authority-readers ValidatingAdmissionPolicyBinding/opengate-authority-readers " \
  "$(kinds_in "$WORK/applied/01.yaml")"
assert_eq "the workflows' access names their user, and nothing is left unfilled" \
  "5 0" "$(grep -c 'ocid1.user.oc1..workflow' "$WORK/applied/02.yaml") $(grep -c 'WORKFLOW_USER_OCID' "$WORK/applied/02.yaml" || true)"
assert_eq "the production issuer is applied with its contact" \
  "ClusterIssuer/letsencrypt-prod  1" \
  "$(kinds_in "$WORK/applied/03.yaml") $(grep -c 'email: ops@example.test' "$WORK/applied/03.yaml")"
assert_eq "monitoring's cluster roles are applied" \
  "ClusterRole/monitoring-promtail ClusterRole/monitoring-victoriametrics ClusterRoleBinding/monitoring-promtail ClusterRoleBinding/monitoring-victoriametrics " \
  "$(kinds_in "$WORK/applied/04.yaml")"
assert_eq "the ingress controller is applied last, keeping its record in a configmap" \
  "ingress apply HELM_DRIVER=configmap" "$(tail -n 1 "$WORK/trace")"

# summary_has TEXT names TEXT as a line of the last run's job summary.
summary_has() {
  [ -f "$WORK/summary" ] && grep -qxF -- "$1" "$WORK/summary"
}

status=0
run >"$WORK/out" 2>&1 || status=$?
for line in "## Admin cluster changes" \
  "**Why it ran:** changed since abc1234: deploy/cluster" \
  "| The namespaces | set0/first unchanged<br>set0/second configured |" \
  "| Helm's release records | opengate: 0 copied, 0 already there<br>opengate-staging: 0 copied, 0 already there<br>monitoring: 0 copied, 0 already there<br>ingress-nginx: 0 copied, 0 already there |" \
  "| The workflows' access | set2/first unchanged<br>set2/second configured |" \
  "| Monitoring's cluster roles | set4/first unchanged<br>set4/second configured |" \
  "| The ingress controller | the ingress controller holds deploy/ingress-nginx/values.yaml at chart 9.9.9 |"; do
  if summary_has "$line"; then pass "the job summary carries: $line"; else fail "the job summary lacks: $line"; fi
done

status=0
run FAKE_REFUSE_AT=2 >"$WORK/out" 2>&1 || status=$?
assert_eq "a refused piece fails the run" "1" "$status"
assert_eq "the job summary marks the refused piece" "yes" \
  "$(summary_has "| The workflows' access | refused: Error from server (Forbidden): refused set 2 |" && echo yes || echo no)"
assert_eq "the job summary stops at the refused piece" "no" \
  "$(summary_has "| The ingress controller | the ingress controller holds deploy/ingress-nginx/values.yaml at chart 9.9.9 |" && echo yes || echo no)"

status=0
run CLUSTER_RUN_REASON= >"$WORK/out" 2>&1 || status=$?
assert_eq "a run told no reason says so" "yes" \
  "$(summary_has "**Why it ran:** not stated by the caller" && echo yes || echo no)"

for input in WORKFLOW_USER_OCID ACME_EMAIL; do
  status=0
  run "$input=" >/dev/null 2>&1 || status=$?
  assert_eq "a run without $input is refused before it touches the cluster" "2 0" \
    "$status $(grep -c . "$WORK/trace" || true)"
done

echo
echo "cluster-apply: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
