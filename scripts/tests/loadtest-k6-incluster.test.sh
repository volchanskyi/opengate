#!/usr/bin/env bash
# Tests for scripts/loadtest-k6-incluster.sh — the shim that runs a k6 scenario
# inside the staging cluster and brings its summary export back to the runner.
#
# Why the shim exists: k6 driven from a GitHub runner reached staging through
# `kubectl port-forward`, which multiplexes every forwarded connection over one
# stream to the API server. Run 31775187300 shows what that costs — 29 requests
# in the first scenario hung to k6's 60s ceiling, and by the second scenario the
# tunnel was gone, so setup's register POST timed out and the whole job aborted.
# The tunnel was also inside the measurement: every latency number in the trend
# carried the runner→API-server→pod hop. Running k6 next to the server removes
# both problems, and this shim is the seam that keeps the keep/discard rule in
# scripts/loadtest-k6-run.sh as the single decision point.
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

# The staging server's in-cluster listener is plaintext — TLS terminates at the
# ingress, in front of the service — so the scenarios address it over http. Named
# once here, and everywhere below by this name.
STAGING_URL="http://opengate-staging-server:8080"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/summaries" "$WORK/pod"

# A kubectl stand-in. `exec` records its argv and writes the export the way real
# k6 does — into the pod, not onto the runner — so the copy-back is the only
# thing that can put a summary where the runner will look. `cp` moves files
# between the fake pod directory and the runner.
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
    prev=""
    for arg in "\$@"; do
      [ "\$prev" = "--summary-export" ] && printf '{"metrics":{}}' >"$WORK/pod/\$(basename "\$arg")"
      prev="\$arg"
    done
    exit "\$(cat "$WORK/k6-exit")"
    ;;
  cp)
    src="\$(printf '%s\n' "\$@" | grep ':' | tail -1)"
    dst="\$(printf '%s\n' "\$@" | tail -1)"
    file="$WORK/pod/\$(basename "\${src#*:}")"
    if [ -f "\$file" ]; then cp "\$file" "\$dst"; else exit 1; fi
    ;;
esac
EOF
chmod +x "$WORK/bin/kubectl"

run_shim() {
  echo "$1" >"$WORK/k6-exit"
  rm -f "$WORK/kubectl-calls.txt" "$WORK/summaries"/*.json "$WORK/pod"/*.json
  STATUS=0
  PATH="$WORK/bin:$PATH" \
    NAMESPACE=opengate-staging \
    LOADTEST_K6_POD=k6-loadtest-1 \
    "$SHIM" run \
    --summary-export "$WORK/summaries/api-baseline.json" \
    --summary-trend-stats "avg,p(95)" \
    --env "BASE_URL=$STAGING_URL" \
    /tmp/load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "loadtest-k6-incluster:"

# A clean run: k6 ran in the pod and its export reached the runner, which is the
# only place scripts/loadtest-k6-run.sh looks for it.
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

# A threshold failure measured the fleet. The export must still come back, or
# the runner would discard a real measurement for want of a file.
run_shim 99
assert_eq "threshold failure propagates 99" "99" "$STATUS"
if [ -s "$WORK/summaries/api-baseline.json" ]; then
  pass "threshold failure still copies the export back"
else
  fail "threshold failure still copies the export back"
fi

# A script exception aborts the scenario. The shim propagates the status and
# still copies whatever k6 wrote, because deciding what to do with a partial
# export belongs to the runner, not here.
run_shim 107
assert_eq "script exception propagates 107" "107" "$STATUS"
if [ -s "$WORK/summaries/api-baseline.json" ]; then
  pass "aborted run still copies the export back for the runner to judge"
else
  fail "aborted run still copies the export back for the runner to judge"
fi

# Composed with the runner, an aborted scenario must end with no row on disk:
# the shim brings the partial export back and the runner throws it away.
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
  K6_SUMMARY_TREND_STATS="avg,p(95)" \
  "$RUNNER" api-baseline /tmp/load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
assert_eq "runner over the shim propagates 107" "107" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  fail "runner over the shim discards an aborted scenario's export"
else
  pass "runner over the shim discards an aborted scenario's export"
fi

# Without a pod there is nothing to exec into; failing here names the cause
# instead of leaving every scenario to fail as a k6 error.
STATUS=0
PATH="$WORK/bin:$PATH" NAMESPACE=opengate-staging \
  "$SHIM" run --summary-export "$WORK/summaries/x.json" /tmp/load/k6/scenarios/api-baseline.js \
  >"$WORK/out.txt" 2>&1 || STATUS=$?
if [ "$STATUS" -ne 0 ] && grep -qi 'LOADTEST_K6_POD' "$WORK/out.txt"; then
  pass "an unset pod name fails with the reason"
else
  fail "an unset pod name fails with the reason"
fi

# The workflow must not reintroduce the tunnel: no port-forward, and the base
# URL k6 is given has to be the in-cluster service rather than a local port.
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
