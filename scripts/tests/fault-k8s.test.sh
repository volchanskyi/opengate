#!/usr/bin/env bash
# Offline tests for the FI5 Kubernetes scenario runner:
#   scripts/fault/pod-delete.sh   (C1 single-pod deletion + 120 s recovery SLO)
#   scripts/fault/bad-rollout.sh  (C2 bad rollout + Helm rollback)
#
# No live cluster: kubectl and helm are stubbed on PATH. The stubs record their
# argv and return env-controlled exit codes so the tests exercise the namespace
# guard, the SLO/rollout assertions, the always-rollback cleanup, idempotency,
# and evidence capture without any cluster access.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PODDEL="$REPO_ROOT/scripts/fault/pod-delete.sh"
BADROLL="$REPO_ROOT/scripts/fault/bad-rollout.sh"

for f in "$PODDEL" "$BADROLL"; do
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
assert_file() {
  local name="$1" path="$2"
  if [ -s "$path" ]; then pass "$name"; else fail "$name (missing/empty [$path])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"

cat >"$BIN_DIR/kubectl" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
verb="${1:-}"
case "$verb" in
  rollout) exit "${MOCK_ROLLOUT_RC:-0}" ;;
  delete) exit "${MOCK_DELETE_RC:-0}" ;;
  wait) exit "${MOCK_WAIT_RC:-0}" ;;
  get) printf 'mock kubectl get: %s\n' "$*" ;;
  *) printf 'mock kubectl: %s\n' "$*" ;;
esac
exit 0
SH
chmod +x "$BIN_DIR/kubectl"

cat >"$BIN_DIR/helm" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${HELM_ARGS:-/dev/null}"
sub="${1:-}"
case "$sub" in
  history)
    if [ -n "${MOCK_HELM_HISTORY:-}" ]; then
      printf '%s\n' "$MOCK_HELM_HISTORY"
    else
      printf '%s\n' '[{"revision":5,"status":"deployed"}]'
    fi
    ;;
  upgrade) exit "${MOCK_HELM_UPGRADE_RC:-1}" ;;
  rollback) exit "${MOCK_HELM_ROLLBACK_RC:-0}" ;;
  status) printf 'mock helm status\n' ;;
  *) : ;;
esac
exit 0
SH
chmod +x "$BIN_DIR/helm"

EVID="$WORK/evidence"

reset_args() {
  : >"$WORK/kubectl.args"
  : >"$WORK/helm.args"
  rm -rf "$EVID"
}

# run_k8s <namespace> <script> — invoke a runner against the mock cluster with
# `env` (no subshell export, so ShellCheck stays quiet). MOCK_* come from the
# caller's per-test prefix assignment.
run_k8s() {
  local ns="$1" script="$2"
  shift 2
  env "PATH=$BIN_DIR:$PATH" \
    "KUBECTL_ARGS=$WORK/kubectl.args" "HELM_ARGS=$WORK/helm.args" \
    "NAMESPACE=$ns" "RELEASE=opengate-staging" "EVIDENCE_DIR=$EVID" \
    "MOCK_ROLLOUT_RC=${MOCK_ROLLOUT_RC:-0}" \
    "MOCK_HELM_UPGRADE_RC=${MOCK_HELM_UPGRADE_RC:-1}" \
    "MOCK_HELM_ROLLBACK_RC=${MOCK_HELM_ROLLBACK_RC:-0}" \
    "$script" "$@"
}

echo "FI5 kubernetes scenario runner:"

# --- C1 pod-delete: happy path ----------------------------------------------
reset_args
rc=0
run_k8s opengate-staging "$PODDEL" >/dev/null 2>&1 || rc=$?
assert_eq "pod-delete recovers within SLO exits 0" "0" "$rc"
assert_contains "pod-delete targets the server pod by exact selector" "app.kubernetes.io/component=server" "$(cat "$WORK/kubectl.args")"
assert_contains "pod-delete deletes a pod" "delete pod" "$(cat "$WORK/kubectl.args")"
assert_contains "pod-delete waits for rollout recovery" "rollout status" "$(cat "$WORK/kubectl.args")"
assert_file "pod-delete captures evidence" "$EVID/pod-delete-events.txt"

# --- C1 pod-delete: SLO breach ----------------------------------------------
reset_args
rc=0
MOCK_ROLLOUT_RC=1 run_k8s opengate-staging "$PODDEL" >/dev/null 2>&1 || rc=$?
assert_ne "pod-delete SLO breach exits non-zero" "0" "$rc"
assert_file "pod-delete still captures evidence on SLO breach" "$EVID/pod-delete-events.txt"

# --- C1 pod-delete: namespace guard -----------------------------------------
reset_args
rc=0
out="$(run_k8s opengate "$PODDEL" 2>&1)" || rc=$?
assert_ne "pod-delete refuses a non-staging namespace" "0" "$rc"
assert_contains "pod-delete names the required staging namespace" "opengate-staging" "$out"
assert_eq "pod-delete touched no cluster under a refused namespace" "" "$(cat "$WORK/kubectl.args")"

# --- C2 bad-rollout: happy path (bad revision fails, rollback restores) ------
reset_args
rc=0
run_k8s opengate-staging "$BADROLL" >/dev/null 2>&1 || rc=$?
assert_eq "bad-rollout + rollback exits 0" "0" "$rc"
assert_contains "bad-rollout deploys a failing revision" "upgrade opengate-staging" "$(cat "$WORK/helm.args")"
assert_contains "bad-rollout rolls back to the captured good revision" "rollback opengate-staging 5" "$(cat "$WORK/helm.args")"
assert_file "bad-rollout captures evidence" "$EVID/bad-rollout-events.txt"

# --- C2 bad-rollout: rollback ALWAYS runs even if the bad rollout unexpectedly succeeds
reset_args
rc=0
MOCK_HELM_UPGRADE_RC=0 run_k8s opengate-staging "$BADROLL" >/dev/null 2>&1 || rc=$?
assert_contains "rollback still runs when the bad rollout does not fail" "rollback opengate-staging 5" "$(cat "$WORK/helm.args")"

# --- C2 bad-rollout: rollback failure surfaces non-zero ----------------------
reset_args
rc=0
MOCK_HELM_ROLLBACK_RC=1 run_k8s opengate-staging "$BADROLL" >/dev/null 2>&1 || rc=$?
assert_ne "bad-rollout rollback failure exits non-zero" "0" "$rc"

# --- C2 bad-rollout: namespace guard ----------------------------------------
reset_args
rc=0
out="$(run_k8s opengate-prod "$BADROLL" 2>&1)" || rc=$?
assert_ne "bad-rollout refuses a non-staging namespace" "0" "$rc"
assert_contains "bad-rollout names the required staging namespace" "opengate-staging" "$out"
assert_eq "bad-rollout touched no release under a refused namespace" "" "$(cat "$WORK/helm.args")"

# --- idempotency: a second happy run behaves identically --------------------
reset_args
rc=0
run_k8s opengate-staging "$PODDEL" >/dev/null 2>&1 || rc=$?
assert_eq "pod-delete is idempotent (second run exits 0)" "0" "$rc"
reset_args
rc=0
run_k8s opengate-staging "$BADROLL" >/dev/null 2>&1 || rc=$?
assert_eq "bad-rollout is idempotent (second run exits 0)" "0" "$rc"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
