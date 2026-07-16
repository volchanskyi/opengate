#!/usr/bin/env bash
# Offline tests for the FI4 ingress fault tooling:
#   scripts/fault/ingress-apply.sh, scripts/fault/ingress-restore.sh, and the
#   version-controlled staging-only templates under deploy/fault/ingress/.
#
# No live cluster: kubectl is stubbed on PATH with a jq-backed mock that keeps
# the target Ingress annotations and the server Deployment replica count in state
# files, so an apply->restore round-trip is exercised for real and asserted to be
# byte-identical. The namespace guard, the render-time production-deny invariant,
# and the "504 via timeout annotation, not a raw nginx snippet" contract are
# checked without any cluster access.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APPLY="$REPO_ROOT/scripts/fault/ingress-apply.sh"
RESTORE="$REPO_ROOT/scripts/fault/ingress-restore.sh"
TEMPLATE="$REPO_ROOT/deploy/fault/ingress/edge-504-timeout.json"

for f in "$APPLY" "$RESTORE"; do
  [ -x "$f" ] || {
    echo "FAIL: $f not executable" >&2
    exit 1
  }
done

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
assert_ne() {
  local name="$1" notwant="$2" got="$3"
  if [ "$notwant" != "$got" ]; then pass "$name"; else fail "$name (unexpected=[$got])"; fi
}
assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if printf '%s\n' "$haystack" | grep -qF "$needle"; then pass "$name"; else fail "$name (missing [$needle])"; fi
}
assert_not_contains() {
  local name="$1" needle="$2" haystack="$3"
  if printf '%s\n' "$haystack" | grep -qF "$needle"; then fail "$name (unexpected [$needle])"; else pass "$name"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"

# jq-backed kubectl mock. Persists Ingress annotations and Deployment replicas so
# apply/restore round-trip faithfully; unhandled calls exit 3 so the tests notice.
cat >"$BIN_DIR/kubectl" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
verb="${1:-}"
shift || true

kind=""
ojson=0
jsonpath=0
patch_file=""
patch_json=""
replicas=""
args=("$@")
n=${#args[@]}
i=0
while [ "$i" -lt "$n" ]; do
  a="${args[$i]:-}"
  case "$a" in
    ingress | ingresses | ing) kind="ingress" ;;
    deploy | deployment | deployments | deploy.apps) kind="deploy" ;;
    -n | --namespace | --type) i=$((i + 1)) ;;
    --patch-file)
      i=$((i + 1))
      patch_file="${args[$i]:-}"
      ;;
    --patch | -p)
      i=$((i + 1))
      patch_json="${args[$i]:-}"
      ;;
    --replicas=*) replicas="${a#--replicas=}" ;;
    --replicas)
      i=$((i + 1))
      replicas="${args[$i]:-}"
      ;;
    -o | --output)
      i=$((i + 1))
      v="${args[$i]:-}"
      [ "$v" = "json" ] && ojson=1
      case "$v" in jsonpath*) jsonpath=1 ;; esac
      ;;
    -o=* | --output=*)
      v="${a#*=}"
      [ "$v" = "json" ] && ojson=1
      case "$v" in jsonpath*) jsonpath=1 ;; esac
      ;;
  esac
  i=$((i + 1))
done

case "$verb" in
  get)
    if [ "$kind" = "ingress" ] && [ "$ojson" -eq 1 ]; then
      jq -n --slurpfile a "$MOCK_ING_ANN" '{metadata:{annotations:$a[0]}}'
    elif [ "$kind" = "deploy" ] && [ "$jsonpath" -eq 1 ]; then
      cat "$MOCK_REPLICAS"
    else
      echo "mock kubectl: unhandled get (kind=$kind ojson=$ojson jsonpath=$jsonpath)" >&2
      exit 3
    fi
    ;;
  patch)
    body=""
    if [ -n "$patch_file" ]; then
      body="$(cat "$patch_file")"
    else
      body="$patch_json"
    fi
    ann="$(printf '%s' "$body" | jq -c '.metadata.annotations // {}')"
    # RFC 7386 semantics for the flat annotations map: null deletes the key.
    jq --argjson p "$ann" '
      reduce ($p | to_entries[]) as $e (.;
        if $e.value == null then del(.[$e.key]) else .[$e.key] = $e.value end)
    ' "$MOCK_ING_ANN" >"$MOCK_ING_ANN.tmp" && mv "$MOCK_ING_ANN.tmp" "$MOCK_ING_ANN"
    ;;
  scale)
    printf '%s' "$replicas" >"$MOCK_REPLICAS"
    ;;
  *)
    echo "mock kubectl: unhandled verb '$verb'" >&2
    exit 3
    ;;
esac
exit 0
SH
chmod +x "$BIN_DIR/kubectl"

MOCK_ING_ANN="$WORK/ing-ann.json"
MOCK_REPLICAS="$WORK/replicas"
STATE_DIR="$WORK/state"

reset_cluster() {
  # A realistic staging Ingress annotation set: two timeout keys the fault will
  # touch, plus an untouched key that must survive apply+restore unchanged.
  cat >"$MOCK_ING_ANN" <<'JSON'
{
  "kubernetes.io/ingress.class": "nginx",
  "nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
  "nginx.ingress.kubernetes.io/proxy-send-timeout": "3600"
}
JSON
  printf '1' >"$MOCK_REPLICAS"
  rm -rf "$STATE_DIR"
}

ann_sorted() { jq -S . "$MOCK_ING_ANN"; }
ann_key() { jq -r --arg k "$1" '.[$k] // "<absent>"' "$MOCK_ING_ANN"; }

# run_fault <namespace> <script> [args...] — invoke a fault script against the
# mock cluster with `env` (no subshell PATH export, so ShellCheck stays quiet).
run_fault() {
  local ns="$1" script="$2"
  shift 2
  env "PATH=$BIN_DIR:$PATH" \
    "MOCK_ING_ANN=$MOCK_ING_ANN" "MOCK_REPLICAS=$MOCK_REPLICAS" \
    "NAMESPACE=$ns" "RELEASE=opengate-staging" "FAULT_STATE_DIR=$STATE_DIR" \
    "$script" "$@"
}

echo "FI4 ingress fault tooling:"

# --- edge-504 round-trip ----------------------------------------------------
reset_cluster
initial="$(ann_sorted)"
rc=0
run_fault opengate-staging "$APPLY" edge-504 >/dev/null 2>&1 || rc=$?
assert_eq "apply edge-504 exits 0" "0" "$rc"
assert_eq "edge-504 lowers proxy-read-timeout" "5" "$(ann_key 'nginx.ingress.kubernetes.io/proxy-read-timeout')"
assert_eq "edge-504 lowers proxy-send-timeout" "5" "$(ann_key 'nginx.ingress.kubernetes.io/proxy-send-timeout')"
assert_eq "edge-504 sets the fault marker" "edge-504-timeout" "$(ann_key 'fault.opengate.dev/scenario')"
assert_eq "edge-504 leaves untouched annotations intact" "nginx" "$(ann_key 'kubernetes.io/ingress.class')"

rc=0
run_fault opengate-staging "$RESTORE" edge-504 >/dev/null 2>&1 || rc=$?
assert_eq "restore edge-504 exits 0" "0" "$rc"
assert_eq "restore edge-504 is byte-identical" "$initial" "$(ann_sorted)"

# restore is idempotent — a trap double-fire must not corrupt or error.
rc=0
run_fault opengate-staging "$RESTORE" edge-504 >/dev/null 2>&1 || rc=$?
assert_eq "second restore edge-504 exits 0" "0" "$rc"
assert_eq "second restore edge-504 stays byte-identical" "$initial" "$(ann_sorted)"

# --- edge-502 round-trip (upstream scaled to zero) --------------------------
reset_cluster
rc=0
run_fault opengate-staging "$APPLY" edge-502 >/dev/null 2>&1 || rc=$?
assert_eq "apply edge-502 exits 0" "0" "$rc"
assert_eq "edge-502 scales the server deployment to zero" "0" "$(cat "$MOCK_REPLICAS")"

rc=0
run_fault opengate-staging "$RESTORE" edge-502 >/dev/null 2>&1 || rc=$?
assert_eq "restore edge-502 exits 0" "0" "$rc"
assert_eq "restore edge-502 restores the original replica count" "1" "$(cat "$MOCK_REPLICAS")"

# --- namespace guard (production-deny on the live path) ----------------------
reset_cluster
rc=0
out="$(run_fault opengate "$APPLY" edge-504 2>&1)" || rc=$?
assert_ne "apply refuses a non-staging namespace" "0" "$rc"
assert_contains "apply names the required staging namespace" "opengate-staging" "$out"
assert_eq "apply made no change under a refused namespace" "3600" "$(ann_key 'nginx.ingress.kubernetes.io/proxy-read-timeout')"

rc=0
out="$(run_fault opengate "$RESTORE" edge-504 2>&1)" || rc=$?
assert_ne "restore refuses a non-staging namespace" "0" "$rc"
assert_contains "restore names the required staging namespace" "opengate-staging" "$out"

rc=0
out="$(run_fault opengate-prod "$APPLY" edge-502 2>&1)" || rc=$?
assert_ne "apply edge-502 refuses a non-staging namespace" "0" "$rc"

# --- argument validation ----------------------------------------------------
rc=0
out="$(run_fault opengate-staging "$APPLY" bogus-scenario 2>&1)" || rc=$?
assert_eq "unknown scenario exits 2" "2" "$rc"
assert_contains "unknown scenario prints usage" "edge-504" "$out"

rc=0
out="$(run_fault opengate-staging "$APPLY" 2>&1)" || rc=$?
assert_eq "missing scenario arg exits 2" "2" "$rc"

# --- render-time production-deny: the chart embeds no fault marker -----------
marker_hits="$(grep -RIl 'fault\.opengate\.dev' "$REPO_ROOT/deploy/helm/opengate/templates" 2>/dev/null || true)"
assert_eq "no chart template embeds a fault-injection annotation" "" "$marker_hits"

# --- 504 template: timeout annotation, never a raw nginx snippet -------------
template_body="$(cat "$TEMPLATE")"
if jq empty "$TEMPLATE" >/dev/null 2>&1; then
  pass "504 template is valid JSON"
else
  fail "504 template is valid JSON"
fi
assert_contains "504 template lowers proxy-read-timeout" "nginx.ingress.kubernetes.io/proxy-read-timeout" "$template_body"
assert_contains "504 template carries the fault marker" "fault.opengate.dev/scenario" "$template_body"
assert_not_contains "504 template uses no configuration-snippet" "configuration-snippet" "$template_body"
assert_not_contains "504 template uses no server-snippet" "server-snippet" "$template_body"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
