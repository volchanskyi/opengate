#!/usr/bin/env bash
# Tests for scripts/loadtest-k6-incluster.sh, which runs k6 in the staging cluster.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SHIM="$REPO_ROOT/scripts/loadtest-k6-incluster.sh"
RUNNER="$REPO_ROOT/scripts/loadtest-k6-run.sh"
[ -x "$SHIM" ] || {
  echo "FAIL: $SHIM not executable" >&2
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

# The in-cluster listener is plaintext because TLS terminates at the ingress.
STAGING_URL="http://opengate-staging-server:8080"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/summaries" "$WORK/pod"

cat >"$WORK/bin/kubectl" <<EOF
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "\$*" >>"$WORK/kubectl-calls.txt"
verb=""
for arg in "\$@"; do
  case "\$arg" in
    exec | cp)
      verb="\$arg"
      break
      ;;
  esac
done
case "\$verb" in
  exec)
    # The shim asks the pod whether the export is there before it says k6 wrote
    # none, so the stand-in answers that question about its own pod directory
    # rather than running k6 again.
    probing=""
    for arg in "\$@"; do
      [ "\$arg" = "test" ] && probing=1
      probe="\$arg"
    done
    if [ -n "\$probing" ]; then
      [ -f "$WORK/pod/\$(basename "\$probe")" ] || exit 1
      exit 0
    fi
    if [ -s "$WORK/drops-left" ] && [ "\$(cat "$WORK/drops-left")" -gt 0 ]; then
      echo \$((\$(cat "$WORK/drops-left") - 1)) >"$WORK/drops-left"
      cat "$WORK/drop-text" >&2
      exit 1
    fi
    prev=""
    for arg in "\$@"; do
      if [ "\$prev" = "--summary-export" ] && [ ! -f "$WORK/k6-writes-nothing" ]; then
        printf '{"metrics":{}}' >"$WORK/pod/\$(basename "\$arg")"
      fi
      prev="\$arg"
    done
    exit "\$(cat "$WORK/k6-exit")"
    ;;
  cp)
    src="\$(printf '%s\n' "\$@" | grep ':' | tail -1)"
    dst="\$(printf '%s\n' "\$@" | tail -1)"
    file="$WORK/pod/\$(basename "\${src#*:}")"
    if [ -f "$WORK/cp-refuses" ]; then
      echo "error: the transport gave out" >&2
      exit 1
    fi
    if [ -f "\$file" ]; then cp "\$file" "\$dst"; else
      echo "error: \$src: no such file or directory" >&2
      exit 1
    fi
    ;;
esac
EOF
chmod +x "$WORK/bin/kubectl"

run_shim() {
  echo "$1" >"$WORK/k6-exit"
  rm -f "$WORK/kubectl-calls.txt" "$WORK/summaries"/*.json "$WORK/pod"/*.json \
    "$WORK/cp-refuses" "$WORK/k6-writes-nothing"
  STATUS=0
  PATH="$WORK/bin:$PATH" \
    KUBECTL_RETRY_DELAY=0 \
    NAMESPACE=opengate-staging \
    LOADTEST_K6_POD=k6-loadtest-1 \
    "$SHIM" run \
    --summary-export "$WORK/summaries/api-baseline.json" \
    --summary-trend-stats "avg,p(95)" \
    --env "BASE_URL=$STAGING_URL" \
    /tmp/load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
}

# k6_starts counts the exec calls that asked the pod to start k6.
k6_starts() {
  grep -c -- 'exec k6-loadtest-1 -- /tmp/k6 run' "$WORK/kubectl-calls.txt" || true
}

echo "loadtest-k6-incluster:"

run_shim 0
assert_eq "clean run exits 0" "0" "$STATUS"
if [ -s "$WORK/summaries/api-baseline.json" ]; then
  pass "clean run copies the summary export back to the runner"
else
  fail "clean run copies the summary export back to the runner"
fi
if grep -q 'exec k6-loadtest-1' "$WORK/kubectl-calls.txt"; then
  pass "k6 runs inside the staging pod"
else
  fail "k6 runs inside the staging pod"
fi
if grep -qF -- "--env BASE_URL=$STAGING_URL" "$WORK/kubectl-calls.txt"; then
  pass "scenario arguments reach k6 unchanged"
else
  fail "scenario arguments reach k6 unchanged"
fi

run_shim 99
assert_eq "threshold failure propagates 99" "99" "$STATUS"
if [ -s "$WORK/summaries/api-baseline.json" ]; then
  pass "threshold failure still copies the export back"
else
  fail "threshold failure still copies the export back"
fi

run_shim 107
assert_eq "script exception propagates 107" "107" "$STATUS"
if [ -s "$WORK/summaries/api-baseline.json" ]; then
  pass "aborted run still copies the export back for the runner to judge"
else
  fail "aborted run still copies the export back for the runner to judge"
fi

# The text kubectl printed when the node closed the connection before the exec started.
NEVER_ARRIVED='error: Internal error occurred: error sending request: Post '
NEVER_ARRIVED+='"https://10.0.2.227:10250/exec/opengate-staging/k6-loadtest-1/k6-loadtest-1": EOF'
echo 1 >"$WORK/drops-left"
printf '%s\n' "$NEVER_ARRIVED" >"$WORK/drop-text"
run_shim 0
assert_eq "a start the node never received is repeated and the scenario passes" "0" "$STATUS"
assert_eq "a start the node never received is tried twice" "2" "$(k6_starts)"
if [ -s "$WORK/summaries/api-baseline.json" ]; then
  pass "a repeated start copies the summary export back"
else
  fail "a repeated start copies the summary export back"
fi

# The text kubectl prints when the connection drops while the command is already running.
echo 1 >"$WORK/drops-left"
printf '%s\n' 'error: error reading from error stream: next reader: EOF' >"$WORK/drop-text"
run_shim 0
if [ "$STATUS" -ne 0 ]; then
  pass "a connection lost after k6 started fails the scenario"
else
  fail "a connection lost after k6 started fails the scenario"
fi
assert_eq "a connection lost after k6 started is never repeated" "1" "$(k6_starts)"
rm -f "$WORK/drops-left"

run_shim 0
: >"$WORK/cp-refuses"
STATUS=0
PATH="$WORK/bin:$PATH" \
  NAMESPACE=opengate-staging \
  LOADTEST_K6_POD=k6-loadtest-1 \
  "$SHIM" run \
  --summary-export "$WORK/summaries/api-baseline.json" \
  --env "BASE_URL=$STAGING_URL" \
  /tmp/load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
assert_eq "a refused copy does not change the scenario's own status" "0" "$STATUS"
if grep -qF 'could not be copied' "$WORK/out.txt" \
  && grep -qF 'the transport gave out' "$WORK/out.txt"; then
  pass "a copy that failed over an export the pod holds says so, and why"
else
  fail "a copy that failed over an export the pod holds says so, and why"
fi
if grep -qF 'wrote no summary export' "$WORK/out.txt"; then
  fail "a refused copy is not reported as k6 writing nothing"
else
  pass "a refused copy is not reported as k6 writing nothing"
fi

run_shim 0
rm -f "$WORK/pod"/*.json "$WORK/summaries"/*.json
: >"$WORK/k6-writes-nothing"
STATUS=0
PATH="$WORK/bin:$PATH" \
  NAMESPACE=opengate-staging \
  LOADTEST_K6_POD=k6-loadtest-1 \
  "$SHIM" run \
  --summary-export "$WORK/summaries/api-baseline.json" \
  --env "BASE_URL=$STAGING_URL" \
  /tmp/load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
if grep -qF 'wrote no summary export' "$WORK/out.txt"; then
  pass "an export the pod does not hold is reported as k6 writing none"
else
  fail "an export the pod does not hold is reported as k6 writing none"
fi

echo 107 >"$WORK/k6-exit"
rm -f "$WORK/summaries"/*.json "$WORK/pod"/*.json
STATUS=0
PATH="$WORK/bin:$PATH" \
  NAMESPACE=opengate-staging \
  LOADTEST_K6_POD=k6-loadtest-1 \
  K6_BIN="$SHIM" \
  LOADTEST_K6_SUMMARY_DIR="$WORK/summaries" \
  LOADTEST_BASE_URL="$STAGING_URL" \
  LOADTEST_RUN_ID="42-1" \
  LOADTEST_PROFILE="$REPO_ROOT/load/profiles/normal.yaml" \
  K6_SUMMARY_TREND_STATS="avg,p(95)" \
  "$RUNNER" api-baseline /tmp/load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
assert_eq "runner over the shim propagates 107" "107" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  fail "runner over the shim discards an aborted scenario's export"
else
  pass "runner over the shim discards an aborted scenario's export"
fi

STATUS=0
PATH="$WORK/bin:$PATH" NAMESPACE=opengate-staging \
  "$SHIM" run --summary-export "$WORK/summaries/x.json" /tmp/load/k6/scenarios/api-baseline.js \
  >"$WORK/out.txt" 2>&1 || STATUS=$?
if [ "$STATUS" -ne 0 ] && grep -qi 'LOADTEST_K6_POD' "$WORK/out.txt"; then
  pass "an unset pod name fails with the reason"
else
  fail "an unset pod name fails with the reason"
fi

WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
pf="$(grep -cE 'port-forward' "$WORKFLOW" || true)"
assert_eq "load-test.yml no longer tunnels to staging" "0" "$pf"
if grep -qF "LOADTEST_BASE_URL=${STAGING_URL%%:*}://\${RELEASE}-server:8080" "$WORKFLOW" \
  && ! grep -q '127\.0\.0\.1:18080' "$WORKFLOW"; then
  pass "k6 addresses the staging service from inside the cluster"
else
  fail "k6 addresses the staging service from inside the cluster"
fi
if grep -q 'loadtest-k6-incluster.sh' "$WORKFLOW"; then
  pass "load-test.yml drives k6 through the in-cluster shim"
else
  fail "load-test.yml drives k6 through the in-cluster shim"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
