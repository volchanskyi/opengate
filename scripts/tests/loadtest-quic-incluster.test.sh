#!/usr/bin/env bash
# Tests for scripts/loadtest-quic-incluster.sh, which holds the QUIC fleet inside the cluster.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SHIM="$REPO_ROOT/scripts/loadtest-quic-incluster.sh"
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
assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then
    pass "$name"
  else
    fail "$name (no [$needle] in [$haystack])"
  fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/pod"

# The fake runs the shim's `sh -c` script in a directory standing in for the pod.
# The exec-fails file counts the next exec calls refused the way the kubelet refuses them.
cat >"$WORK/bin/kubectl" <<EOF
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "\$*" >>"$WORK/kubectl-calls.txt"

fails="\$(cat "$WORK/exec-fails" 2>/dev/null || echo 0)"
if [ "\$fails" -gt 0 ]; then
  printf '%s\n' "\$((fails - 1))" >"$WORK/exec-fails"
  echo 'error: Internal error occurred: error sending request: Post "https://10.0.2.227:10250/exec/opengate-staging/quic-loadtest/quic-loadtest": EOF' >&2
  exit 1
fi

script=""
prev=""
shift_count=0
for arg in "\$@"; do
  shift_count=\$((shift_count + 1))
  if [ "\$prev" = "-c" ]; then
    script="\$arg"
    break
  fi
  prev="\$arg"
done
shift "\$shift_count"
exec sh -c "\$script" "\$@"
EOF
chmod +x "$WORK/bin/kubectl"

cat >"$WORK/bin/harness" <<EOF
#!/usr/bin/env bash
set -uo pipefail
printf 'ran\n' >>"$WORK/harness-runs.txt"
exit_code="\${HARNESS_EXIT:-0}"
announce="\${HARNESS_ANNOUNCE:-1}"
if [ "\$announce" = "1" ]; then
  echo 'Starting QUIC load test: 100 agents across 1 tenant(s) → opengate-staging-server:9090'
fi
sleep "\${HARNESS_HOLD:-0.3}"
if [ "\${HARNESS_FILES:-1}" = "1" ]; then
  echo 'Estate filed'
fi
if [ "\$announce" = "1" ]; then
  cat <<'BODY'

=== Results ===
Total time:  0.3s
Agents:      100/100 succeeded
Failures:    0
BODY
fi
printf 'done\n' >>"$WORK/harness-ends.txt"
exit "\$exit_code"
EOF
chmod +x "$WORK/bin/harness"

POD_LOG="$WORK/pod/fleet.log"
POD_STATUS="$WORK/pod/fleet.status"

export PATH="$WORK/bin:$PATH"
export LOADTEST_POD="quic-loadtest-1"
export NAMESPACE="opengate-staging"
export LOADTEST_QUIC_POD_LOG="$POD_LOG"
export LOADTEST_QUIC_POD_STATUS="$POD_STATUS"
export LOADTEST_QUIC_POLL_SECONDS="0.2"
export LOADTEST_QUIC_START_TIMEOUT_SECONDS="15"
export LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS="15"
export LOADTEST_QUIC_START_ATTEMPTS="3"

lines_in() {
  [ -f "$1" ] || {
    echo 0
    return 0
  }
  wc -l <"$1" | tr -d ' '
}

# The launcher writes the exit code to a fixed pod path, so a harness still running lands it in the
# next case's run.
settle() {
  local deadline=$((SECONDS + 10)) started ended
  while [ "$SECONDS" -lt "$deadline" ]; do
    started="$(lines_in "$WORK/harness-runs.txt")"
    ended="$(lines_in "$WORK/harness-ends.txt")"
    [ "$started" = "$ended" ] && return 0
    sleep 0.1
  done
  return 0
}

reset_pod() {
  settle
  rm -f "$POD_LOG" "$POD_STATUS" "$WORK/harness-runs.txt" "$WORK/harness-ends.txt" \
    "$WORK/kubectl-calls.txt" "$WORK/exec-fails"
}

harness_runs() {
  wc -l <"$WORK/harness-runs.txt" 2>/dev/null | tr -d ' '
}

echo "loadtest-quic-incluster:"

reset_pod
STATUS=0
OUT="$("$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
assert_eq "a fleet that came up starts clean" "0" "$STATUS"
assert_contains "the start says the fleet is holding" "holding" "$OUT"
assert_eq "the harness ran once" "1" "$(harness_runs)"

STATUS=0
OUT="$("$SHIM" collect 2>&1)" || STATUS=$?
assert_eq "collect returns the harness's own verdict" "0" "$STATUS"
assert_contains "collect prints the harness's results block" "=== Results ===" "$OUT"
assert_contains "collect prints what the harness announced" "Starting QUIC load test" "$OUT"

reset_pod
printf '2\n' >"$WORK/exec-fails"
STATUS=0
OUT="$("$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
assert_eq "a refused launch is made again" "0" "$STATUS"
assert_eq "the retry starts one fleet, not two" "1" "$(harness_runs)"
assert_contains "the refusal is named rather than swallowed" "refused" "$OUT"

reset_pod
STATUS=0
"$SHIM" start -- "$WORK/bin/harness" >/dev/null 2>&1 || STATUS=$?
assert_eq "the first fleet is up" "0" "$STATUS"
printf '1\n' >"$WORK/exec-fails"
STATUS=0
OUT="$("$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
assert_eq "a refusal over a live fleet still reports the fleet" "0" "$STATUS"
assert_eq "no second fleet is started against the first one's fixture" "1" "$(harness_runs)"

reset_pod
STATUS=0
OUT="$(HARNESS_ANNOUNCE=0 HARNESS_EXIT=1 HARNESS_HOLD=0 \
  "$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
if [ "$STATUS" -ne 0 ]; then
  pass "a harness that died before offering a fleet fails the start"
else
  fail "a harness that died before offering a fleet fails the start"
fi
assert_eq "a harness that died is not started again" "1" "$(harness_runs)"

STATUS=0
"$SHIM" collect >/dev/null 2>&1 || STATUS=$?
assert_eq "collect carries the dead harness's exit code" "1" "$STATUS"

reset_pod
START="$SECONDS"
STATUS=0
OUT="$("$SHIM" collect 2>&1)" || STATUS=$?
ELAPSED=$((SECONDS - START))
if [ "$STATUS" -ne 0 ]; then
  pass "collect refuses a pod that holds no fleet"
else
  fail "collect refuses a pod that holds no fleet"
fi
if [ "$ELAPSED" -lt 5 ]; then
  pass "collect answers at once rather than waiting out its bound"
else
  fail "collect answers at once rather than waiting out its bound (took ${ELAPSED}s)"
fi
assert_contains "collect names what is missing" "no fleet" "$OUT"

reset_pod
"$SHIM" start -- "$WORK/bin/harness" >/dev/null 2>&1
printf '1\n' >"$WORK/exec-fails"
STATUS=0
OUT="$("$SHIM" collect 2>&1)" || STATUS=$?
assert_eq "a refused question does not void a live fleet" "0" "$STATUS"
assert_contains "collect still returns the harness's own account" "=== Results ===" "$OUT"

reset_pod
STATUS=0
OUT="$("$SHIM" collect 2>&1)" || STATUS=$?
assert_eq "a pod holding no fleet is still refused" "4" "$STATUS"
assert_contains "and it says what is missing" "no fleet" "$OUT"

reset_pod
printf '9\n' >"$WORK/exec-fails"
STATUS=0
OUT="$("$SHIM" collect 2>&1)" || STATUS=$?
assert_eq "a pod that will not answer is refused" "4" "$STATUS"
assert_contains "and the refusal names the unanswered question" "did not answer" "$OUT"

reset_pod
STATUS=0
"$SHIM" start >/dev/null 2>&1 || STATUS=$?
assert_eq "a start with no command is refused" "2" "$STATUS"
STATUS=0
"$SHIM" >/dev/null 2>&1 || STATUS=$?
assert_eq "no subcommand is refused" "2" "$STATUS"
STATUS=0
LOADTEST_POD="" "$SHIM" collect >/dev/null 2>&1 || STATUS=$?
assert_eq "an unnamed pod is refused" "2" "$STATUS"

WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
quic_start_block="$(awk '
  /^[[:space:]]*- name: Start the QUIC fleet and hold it connected/ { found = 1; next }
  found && /^[[:space:]]*- name:/ { exit }
  found { print }
' "$WORKFLOW")"

if grep -q 'loadtest-quic-incluster.sh start' <<<"$quic_start_block"; then
  pass "the workflow launches the fleet through the seam"
else
  fail "the workflow must launch the fleet through scripts/loadtest-quic-incluster.sh"
fi

if grep -qE 'nohup|kubectl[^|]*exec' <<<"$quic_start_block"; then
  fail "the workflow still holds the fleet on a stream it owns"
else
  pass "the workflow holds no stream open for the fleet"
fi

if grep -qE '^[[:space:]]*sleep[[:space:]]' <<<"$quic_start_block"; then
  fail "the start step still waits by sleeping instead of by asking"
else
  pass "the start step proves the fleet is up rather than sleeping"
fi

fleet_wait_block="$(awk '
  /^[[:space:]]*- name: Wait for the QUIC fleet to finish/ { found = 1; next }
  found && /^[[:space:]]*- name:/ { exit }
  found { print }
' "$WORKFLOW")"
if grep -q 'loadtest-quic-incluster.sh collect' <<<"$fleet_wait_block" \
  && grep -q 'loadtest-quic-run.sh' <<<"$fleet_wait_block"; then
  pass "the verdict is collected through the seam and judged by the keep-or-discard rule"
else
  fail "the wait step must collect through the seam and judge with scripts/loadtest-quic-run.sh"
fi

reset_pod
"$SHIM" start -- harness >/dev/null 2>&1
rc=0
"$SHIM" await-filed >/dev/null 2>&1 || rc=$?
assert_eq "a filed estate is waited for and found" "0" "$rc"

reset_pod
HARNESS_FILES=0 HARNESS_HOLD=0.2 "$SHIM" start -- harness >/dev/null 2>&1
rc=0
LOADTEST_QUIC_FILED_TIMEOUT_SECONDS=2 "$SHIM" await-filed >/dev/null 2>&1 || rc=$?
if [ "$rc" -ne 0 ]; then
  pass "an estate that was never filed is refused rather than waited out"
else
  fail "an estate that was never filed was reported as filed"
fi

filed_step="$(awk '
  /^[[:space:]]*- name: .*[Ee]state/ { found = 1 }
  found && /loadtest-quic-incluster.sh await-filed/ { print; exit }
' "$WORKFLOW")"
if [ -n "$filed_step" ]; then
  pass "the workflow waits for the estate to be filed"
else
  fail "the workflow starts its scenarios without waiting for the estate to be filed"
fi

filed_at="$(grep -n 'await-filed' "$WORKFLOW" || true)"
filed_line="${filed_at%%:*}"
scenarios_at="$(grep -n 'Run the browser-side scenarios against the walk' "$WORKFLOW" || true)"
scenarios_line="${scenarios_at%%:*}"
if [ -n "$filed_line" ] && [ -n "$scenarios_line" ] && [ "$filed_line" -lt "$scenarios_line" ]; then
  pass "the estate is filed before the scenarios read it"
else
  fail "the wait for a filed estate must come before the k6 scenarios"
fi

reset_pod
HARNESS_FILES=0 HARNESS_HOLD=0.2 "$SHIM" start -- harness >/dev/null 2>&1
rc=0
started="$("$SHIM" walk-started-at 2>/dev/null)" || rc=$?
if [ "$rc" -ne 0 ] && [ -z "$started" ]; then
  pass "a harness that never said when it started walking is refused"
else
  fail "a harness that never said when it started walking answered '$started'"
fi

if grep -q 'walk-started-at' "$WORKFLOW"; then
  pass "the workflow asks how far the walk has gone before starting a generator"
else
  fail "the workflow asks how far the walk has gone before starting a generator"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
