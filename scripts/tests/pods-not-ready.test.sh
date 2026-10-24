#!/usr/bin/env bash
# Tests deploy/scripts/pods-not-ready.sh: each container short of ready is named with its reason
# and last lines, an unscheduled pod with the scheduler's message, and no answer fails.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCRIPT="$ROOT/deploy/scripts/pods-not-ready.sh"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# The cluster's answer: a ready store, a dashboard failing each start, a store with no room on the
# node, and a probe whose setup container finished.
cat >"$WORK/pods.json" <<'JSON'
{"items": [
  {"metadata": {"name": "store-0"}, "status": {"phase": "Running",
    "containerStatuses": [{"name": "store", "ready": true, "restartCount": 0, "state": {"running": {}}}]}},
  {"metadata": {"name": "dash-9cd5"}, "status": {"phase": "Running",
    "containerStatuses": [{"name": "dash", "ready": false, "restartCount": 7,
      "state": {"waiting": {"reason": "CrashLoopBackOff", "message": "back-off 5m0s restarting failed container"}},
      "lastState": {"terminated": {"reason": "Error", "exitCode": 255}}}]}},
  {"metadata": {"name": "logs-0"}, "status": {"phase": "Pending",
    "conditions": [{"type": "PodScheduled", "status": "False", "reason": "Unschedulable",
      "message": "0/1 nodes are available: 1 Insufficient memory."}]}},
  {"metadata": {"name": "probe-x1"}, "status": {"phase": "Running",
    "initContainerStatuses": [{"name": "setup", "ready": true, "restartCount": 0,
      "state": {"terminated": {"reason": "Completed", "exitCode": 0}}}],
    "containerStatuses": [{"name": "probe", "ready": false, "restartCount": 0, "state": {"running": {}}}]}}
]}
JSON
# --previous reads the run before a container's own; for one that never restarted, kubectl refuses
# it, as the real one does.
cat >"$WORK/bin/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
[ -z "${FAKE_UNREACHABLE:-}" ] || {
  echo "Unable to connect to the server: dial tcp 10.0.0.1:6443: i/o timeout" >&2
  exit 1
}
args=" $* "
case "$args" in
  *" get pods -o json "*) cat "$FAKE_PODS" ;;
  *" logs dash-9cd5 -c dash --previous "*) printf 'exec /run.sh: exec format error\n' ;;
  *" logs dash-9cd5 -c dash "*)
    printf 'unable to retrieve container logs\n' >&2
    exit 1
    ;;
  *" logs probe-x1 -c probe --previous "*)
    echo 'Error from server (BadRequest): previous terminated container "probe" not found' >&2
    exit 1
    ;;
  *" logs probe-x1 -c probe "*) printf 'listening on :9100\nreadiness: database unreachable\n' ;;
  *)
    echo "unexpected kubectl $*" >&2
    exit 64
    ;;
esac
KUBECTL
chmod +x "$WORK/bin/kubectl"

run() { # VAR=value overrides for the stand-in
  env PATH="$WORK/bin:$PATH" KUBECTL_RETRY_DELAY=0 KUBECTL_RETRY_ATTEMPTS=1 FAKE_PODS="$WORK/pods.json" "$@" \
    bash "$SCRIPT" monitoring >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "pods-not-ready:"

status=0
run || status=$?
if [ "$status" -eq 0 ] \
  && out_has '::error::pods-not-ready: monitoring/dash-9cd5 dash: CrashLoopBackOff (back-off 5m0s restarting failed container); last ended Error, exit 255' \
  && out_has 'exec /run.sh: exec format error'; then
  pass "a restarting container is named with its reason and the last lines its run before wrote"
else
  fail "the restarting container must be named with its log (status=$status out=[$(cat "$WORK/out")])"
fi
if out_has '::error::pods-not-ready: monitoring/probe-x1 probe: running, not ready' \
  && out_has 'readiness: database unreachable' && ! out_has 'setup'; then
  pass "a running container short of ready is named with its own lines, and a finished one is left out"
else
  fail "the running container must be named and the finished one left out (out=[$(cat "$WORK/out")])"
fi
if out_has '::error::pods-not-ready: monitoring/logs-0: Pending: 0/1 nodes are available: 1 Insufficient memory.'; then
  pass "a pod no container of which started is named with the scheduler's message"
else
  fail "the unscheduled pod must be named (out=[$(cat "$WORK/out")])"
fi
if ! out_has 'store'; then
  pass "a ready pod is left out"
else
  fail "the ready pod must be left out (out=[$(cat "$WORK/out")])"
fi

printf '{"items": [{"metadata": {"name": "store-0"}, "status": {"phase": "Running", "containerStatuses":
  [{"name": "store", "ready": true, "restartCount": 0, "state": {"running": {}}}]}}]}\n' >"$WORK/ready.json"
status=0
run FAKE_PODS="$WORK/ready.json" || status=$?
if [ "$status" -eq 0 ] && out_has 'every pod in monitoring is ready'; then
  pass "a namespace whose pods are all ready says so"
else
  fail "an all-ready namespace must say so (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run FAKE_UNREACHABLE=1 || status=$?
if [ "$status" -eq 1 ] && out_has 'the pods of monitoring could not be read'; then
  pass "a cluster that gives no answer about the namespace's pods fails, by name"
else
  fail "an unread pod list must fail (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
