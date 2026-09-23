#!/usr/bin/env bash
# The short cluster calls a nightly cannot afford to lose, and the ones it must
# not repeat.
#
# A drill died three minutes and forty-nine seconds in because a `chmod` inside
# a pod that was already created and ready had its connection dropped: `Internal
# error occurred: error sending request: ... EOF`. Twelve steps of setup, then
# nothing measured. The health check seven lines below it retries sixty times
# over two minutes; the three calls above it get one attempt each. The step
# already knew the cluster was unreliable and guarded the wrong half. The same
# signature has cost three nights across the drill and the load tests.
#
# The retry is narrow on purpose, and the narrowness is the interesting half.
# Three kinds of call in these workflows must never be repeated: the drill's
# probes, whose failure *is* the measurement it is taking during a deliberate
# network fault; the long-lived execs carrying the workload, because a dropped
# `kubectl exec` does not kill the process in the pod and a second attempt runs
# a second k6 against the same server; and the non-idempotent SQL writes, where
# a transport drop cannot say whether the statement landed.
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

# --- the stub kubectl ---------------------------------------------------------
#
# Counts its own invocations in a file, so what the helper did is read off the
# count rather than inferred from its output. The failure text is the real one,
# copied from the run that cost the night.
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

# run_retry FAIL_TIMES FAIL_TEXT ARGS... — drives the helper, records its
# output and its exit status, and leaves the attempt count in $WORK/count.
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

# --- a dropped connection is survived -----------------------------------------
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

# --- a transport that never comes back: it gives up, loudly -------------------
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

# --- a refusal that will never succeed is not retried -------------------------
#
# A missing pod does not become present by asking again, and four attempts at it
# is four times the wait before the real reason is printed.
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

# --- the other transport signatures the sweep of failed nights turned up ------
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

# --- a stdin-carrying call is given its input on every attempt ----------------
#
# A retry that replays the command but not what was piped into it delivers an
# empty file into the pod and reports success.
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

# --- a call with no input does not wait for one -------------------------------
#
# A library that decides whether there is input to read by asking "is standard
# input a terminal" answers yes for every call made from a script, and then waits
# forever on a pipe nobody is writing to. The call never runs, the step never
# ends, and the night is lost to the thing that was meant to save it.
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
