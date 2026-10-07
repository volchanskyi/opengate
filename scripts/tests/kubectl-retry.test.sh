#!/usr/bin/env bash
# Holds the kubectl retry helper to retrying only transport drops, and never a command's answer.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LIB="$REPO_ROOT/scripts/lib/kubectl-retry.sh"

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

[ -f "$LIB" ] || {
  echo "FAIL: $LIB missing" >&2
  exit 1
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# The stub counts its own invocations in a file, so the attempts are read off the count.
cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
set -uo pipefail
n=$(($(cat "$FAKE_COUNT" 2>/dev/null || echo 0) + 1))
printf '%s' "$n" >"$FAKE_COUNT"

if [ "$n" -le "${FAKE_FAIL_TIMES:-0}" ]; then
  printf '%s\n' "${FAKE_FAIL_TEXT}" >&2
  exit 1
fi
echo "ok from kubectl"
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

run_retry() {
  local times="$1" text="$2"
  shift 2
  : >"$WORK/count"
  PATH="$WORK/bin:$PATH" \
    FAKE_COUNT="$WORK/count" \
    FAKE_FAIL_TIMES="$times" \
    FAKE_FAIL_TEXT="$text" \
    KUBECTL_RETRY_DELAY=0 \
    KUBECTL_RETRY_ATTEMPTS=4 \
    bash -c 'set -uo pipefail; . "$1"; shift; kubectl_retry "$@"' _ "$LIB" "$@" \
    >"$WORK/out" 2>&1 </dev/null
}

attempts() { cat "$WORK/count" 2>/dev/null || echo 0; }

EOF_ERROR='error: Internal error occurred: error sending request: Post "https://10.0.2.227:10250/exec/default/netdrill-shaper/shaper?command=chmod": EOF'

echo "kubectl retry:"

if run_retry 2 "$EOF_ERROR" exec -i shaper -- chmod 0755 /tmp/netfault; then
  pass "a call that fails twice on the transport then succeeds is survived"
else
  fail "a call that fails twice on the transport then succeeds is survived (out=[$(cat "$WORK/out")])"
fi
if [ "$(attempts)" = "3" ]; then
  pass "and took exactly the three attempts it needed"
else
  fail "and took exactly the three attempts it needed (attempts=$(attempts))"
fi
if grep -qF "ok from kubectl" "$WORK/out"; then
  pass "and the successful attempt's output reaches the caller"
else
  fail "and the successful attempt's output reaches the caller (out=[$(cat "$WORK/out")])"
fi

if run_retry 99 "$EOF_ERROR" exec -i shaper -- chmod 0755 /tmp/netfault; then
  fail "a transport that never recovers still fails"
else
  pass "a transport that never recovers still fails"
fi
if [ "$(attempts)" = "4" ]; then
  pass "and stops at the budget rather than forever"
else
  fail "and stops at the budget rather than forever (attempts=$(attempts))"
fi
if grep -qF "Internal error occurred" "$WORK/out"; then
  pass "and the last error it saw is what it reports"
else
  fail "and the last error it saw is what it reports (out=[$(cat "$WORK/out")])"
fi

if run_retry 99 'Error from server (NotFound): pods "netdrill-shaper" not found' exec -i netdrill-shaper -- true; then
  fail "a missing pod still fails"
else
  pass "a missing pod still fails"
fi
if [ "$(attempts)" = "1" ]; then
  pass "and is attempted exactly once"
else
  fail "and is attempted exactly once (attempts=$(attempts))"
fi

if run_retry 99 'Error from server (Forbidden): pods is forbidden' get pods; then
  fail "a refusal the credentials cannot fix still fails"
else
  pass "a refusal the credentials cannot fix still fails"
fi
if [ "$(attempts)" = "1" ]; then
  pass "and is attempted exactly once"
else
  fail "and is attempted exactly once (attempts=$(attempts))"
fi

for signature in \
  'Unable to connect to the server: net/http: TLS handshake timeout' \
  'error: unexpected EOF' \
  'Get "https://10.0.2.227:6443/api": dial tcp 10.0.2.227:6443: connect: connection refused' \
  'error: read tcp 10.0.2.1:55000->10.0.2.227:10250: read: connection reset by peer' \
  'Error from server: etcdserver: request timed out'; do
  if run_retry 1 "$signature" get pod loadtest -o jsonpath='{.status.phase}'; then
    pass "survives: ${signature:0:48}"
  else
    fail "survives: ${signature:0:48} (out=[$(cat "$WORK/out")])"
  fi
done

KUBECTL_RETRY_LOST="$(bash -c '. "$1"; printf "%s" "$KUBECTL_RETRY_LOST"' _ "$LIB")"
run_retry 99 "$EOF_ERROR" exec -i shaper -- true && status=0 || status=$?
assert_status() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
assert_status "a transport that never recovers exits with the lost-connection status" \
  "$KUBECTL_RETRY_LOST" "$status"
run_retry 99 'Error from server (NotFound): pods "x" not found' get pod x && status=0 || status=$?
if [ "$status" != "0" ] && [ "$status" != "$KUBECTL_RETRY_LOST" ]; then
  pass "a refusal keeps its own status rather than the lost-connection one"
else
  fail "a refusal keeps its own status rather than the lost-connection one (status=$status)"
fi

if run_retry 99 "$(printf 'curl: (7) Failed to connect to 10.244.0.22 port 9091: connection refused\ncommand terminated with exit code 7')" \
  exec -i probe -- curl http://10.244.0.22:9091/healthz; then
  fail "a command that ran and failed still fails"
else
  pass "a command that ran and failed still fails"
fi
assert_status "and is attempted once, whatever words it used" "1" "$(attempts)"

run_unstarted() {
  local times="$1" text="$2"
  shift 2
  run_retry "$times" "$text" --unstarted "$@"
}
if run_unstarted 1 "$EOF_ERROR" exec -i probe -- curl -X POST http://shaper/impair; then
  pass "a request that never reached the node is asked again"
else
  fail "a request that never reached the node is asked again (out=[$(cat "$WORK/out")])"
fi
assert_status "and took the two attempts it needed" "2" "$(attempts)"
for signature in 'error: unexpected EOF' 'error: client connection lost' \
  'error: read tcp 10.0.2.1:55000->10.0.2.227:10250: read: connection reset by peer'; do
  run_unstarted 99 "$signature" exec -i probe -- curl -X POST http://shaper/impair || true
  assert_status "a connection lost part-way through is not asked again: ${signature:0:40}" "1" "$(attempts)"
done

: >"$WORK/count"
if FAKE_COUNT="$WORK/count" FAKE_FAIL_TIMES=0 FAKE_FAIL_TEXT="" \
  KUBECTL_RETRY_BIN="$WORK/bin/kubectl" \
  bash -c 'set -uo pipefail; . "$1"; PATH=/usr/bin:/bin kubectl_retry get pods' _ "$LIB" >/dev/null 2>&1 \
  && [ "$(attempts)" = "1" ]; then
  pass "the kubectl named in KUBECTL_RETRY_BIN is the one it runs"
else
  fail "the kubectl named in KUBECTL_RETRY_BIN is the one it runs (attempts=$(attempts))"
fi

# A retry replays the piped input too, or an empty file reaches the pod and reports success.
: >"$WORK/count"
PATH="$WORK/bin:$PATH" \
  FAKE_COUNT="$WORK/count" \
  FAKE_FAIL_TIMES=1 \
  FAKE_FAIL_TEXT="$EOF_ERROR" \
  KUBECTL_RETRY_DELAY=0 \
  bash -c 'set -uo pipefail; . "$1"; printf "payload\n" | kubectl_retry --stdin exec -i shaper -- tee /tmp/x' _ "$LIB" \
  >"$WORK/stdin.out" 2>&1 || true
if [ "$(attempts)" = "2" ]; then
  pass "a call carrying stdin is retried rather than abandoned"
else
  fail "a call carrying stdin is retried rather than abandoned (attempts=$(attempts))"
fi

# A call that declares no input must not wait on a standard input pipe nobody writes to.
: >"$WORK/count"
cat >"$WORK/no-input.sh" <<DRIVER
set -uo pipefail
. "$LIB"
kubectl_retry get pod loadtest
DRIVER
if printf '' | PATH="$WORK/bin:$PATH" \
  FAKE_COUNT="$WORK/count" \
  FAKE_FAIL_TIMES=0 \
  FAKE_FAIL_TEXT="" \
  KUBECTL_RETRY_DELAY=0 \
  timeout 10 bash "$WORK/no-input.sh" >/dev/null 2>&1; then
  pass "a call declaring no input returns rather than waiting on one"
else
  fail "a call declaring no input returns rather than waiting on one"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
