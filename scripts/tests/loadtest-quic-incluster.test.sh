#!/usr/bin/env bash
# Tests for scripts/loadtest-quic-incluster.sh — the seam that holds the QUIC
# fleet inside the cluster rather than on the end of a stream the runner owns.
#
# Why the seam exists: the fleet used to be held by one `kubectl exec` running
# for the whole k6 window, backgrounded on the runner. Run 33856943325 shows
# what that costs — the API server's request to the kubelet came back `EOF`, the
# harness never ran, and the step that launched it slept forty-five seconds and
# reported success. Two k6 scenarios then measured a system with no fleet beside
# it before the relay scenario failed for want of a machine, four minutes after
# the fact, and the night was scored invalid.
#
# The k6 half learned this already: it stopped reaching staging through a
# `kubectl port-forward` tunnel because the tunnel was both fragile and inside
# the measurement. This is the same move for the fleet — the harness is launched
# detached in the pod and its verdict is read back afterwards, so no single
# stream carries the run.
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

# A kubectl stand-in. The shim speaks to the pod only through `sh -c`, so the
# fake runs that script for real in a directory standing in for the pod: the
# launcher text under test is the launcher that runs, and a harness it detaches
# genuinely outlives the call that started it.
#
# `exec-fails` is how many of the next exec calls are refused the way the API
# server's request to the kubelet was refused — the failure this seam exists to
# survive.
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

# A harness stand-in. It announces itself the way the real one does once its
# fixture is built, records that it ran, holds briefly, then prints a results
# block and exits with the code the case asks for.
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

# lines_in counts a file that may not exist yet, without the shell complaining
# about the redirect it could not open.
lines_in() {
  [ -f "$1" ] || {
    echo 0
    return 0
  }
  wc -l <"$1" | tr -d ' '
}

# settle waits for a harness an earlier case detached to finish.
#
# The launcher writes the harness's exit code to a fixed path in the pod, so one
# still running when the next case resets the pod lands its status in that
# case's run — which reads as a harness that exited before it did anything. The
# stand-in records its own end, so this is a wait for a fact rather than for a
# duration.
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

# --- A start that worked -------------------------------------------------------
#
# The launch returns immediately and the shim waits for the harness's own
# announcement, so the step's success means a fleet is being offered rather than
# that a sleep expired.
reset_pod
STATUS=0
OUT="$("$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
assert_eq "a fleet that came up starts clean" "0" "$STATUS"
assert_contains "the start says the fleet is holding" "holding" "$OUT"
assert_eq "the harness ran once" "1" "$(harness_runs)"

# The launching call is long gone by the time the harness finishes, which is the
# whole point: no stream carries the run.
STATUS=0
OUT="$("$SHIM" collect 2>&1)" || STATUS=$?
assert_eq "collect returns the harness's own verdict" "0" "$STATUS"
assert_contains "collect prints the harness's results block" "=== Results ===" "$OUT"
assert_contains "collect prints what the harness announced" "Starting QUIC load test" "$OUT"

# --- A launch the kubelet refused ----------------------------------------------
#
# Nothing was started, so nothing was built and the launch is safe to make
# again. This is run 33856943325 exactly.
reset_pod
printf '2\n' >"$WORK/exec-fails"
STATUS=0
OUT="$("$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
assert_eq "a refused launch is made again" "0" "$STATUS"
assert_eq "the retry starts one fleet, not two" "1" "$(harness_runs)"
assert_contains "the refusal is named rather than swallowed" "refused" "$OUT"

# --- A refusal after the fleet is already up -----------------------------------
#
# A second harness would build a second fixture against names the first already
# took, and the customer name a fixture asks for is unique inside its tenant. So
# the retry is decided by asking the pod what it holds, never by the error text.
reset_pod
STATUS=0
"$SHIM" start -- "$WORK/bin/harness" >/dev/null 2>&1 || STATUS=$?
assert_eq "the first fleet is up" "0" "$STATUS"
printf '1\n' >"$WORK/exec-fails"
STATUS=0
OUT="$("$SHIM" start -- "$WORK/bin/harness" 2>&1)" || STATUS=$?
assert_eq "a refusal over a live fleet still reports the fleet" "0" "$STATUS"
assert_eq "no second fleet is started against the first one's fixture" "1" "$(harness_runs)"

# --- A harness that started and died -------------------------------------------
#
# It may have built part of a fixture, so it is reported rather than started
# again, and its own output is what the report carries.
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

# Its verdict is still the harness's own, so the keep-or-discard rule downstream
# reads what the harness said rather than what the shim made of it.
STATUS=0
"$SHIM" collect >/dev/null 2>&1 || STATUS=$?
assert_eq "collect carries the dead harness's exit code" "1" "$STATUS"

# --- A pod that was never given a fleet ----------------------------------------
#
# The collect step runs on every path, including the one where the start failed.
# Waiting out its whole bound there would spend the job's remaining minutes to
# learn what the pod could have said at once.
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

# --- A start with nothing to launch --------------------------------------------
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

# --- The workflow drives the fleet through this seam ---------------------------
#
# A `nohup … kubectl exec … &` in the workflow is the arrangement this replaces:
# the fleet dies with the stream, and the step that started it cannot tell.
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

# A sleep is not a readiness check: it reports success on a fleet that never
# started, which is what let two scenarios measure a system with nothing beside
# it.
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

# --- Waiting for the estate to be filed ---------------------------------------
#
# A machine is filed under its customer and into one of that customer's
# buildings once it has registered, because the row it is filed against does not
# exist before then. The browser-side scenarios narrow every device read to a
# building, and they choose which building in their own setup — once, before
# their first iteration — so a scenario started against an unfiled fleet reads an
# empty building for the whole of its run, whatever is filed afterwards.
#
# So the ordering is asked for rather than assumed, the same way the fleet's own
# readiness is.
reset_pod
"$SHIM" start -- harness >/dev/null 2>&1
rc=0
"$SHIM" await-filed >/dev/null 2>&1 || rc=$?
assert_eq "a filed estate is waited for and found" "0" "$rc"

# A harness that files nothing must not be reported as one that did. The
# scenarios behind this wait would otherwise run against an unfiled fleet and
# publish the empty reads as the night's numbers.
reset_pod
HARNESS_FILES=0 HARNESS_HOLD=0.2 "$SHIM" start -- harness >/dev/null 2>&1
rc=0
LOADTEST_QUIC_FILED_TIMEOUT_SECONDS=2 "$SHIM" await-filed >/dev/null 2>&1 || rc=$?
if [ "$rc" -ne 0 ]; then
  pass "an estate that was never filed is refused rather than waited out"
else
  fail "an estate that was never filed was reported as filed"
fi

# The workflow has to ask, or the shim's verb is a capability nobody uses. The
# ask belongs between the fleet starting and the first scenario running: those
# are the two events it orders.
filed_step="$(awk '
  /^[[:space:]]*- name: .*[Ee]state/ { found = 1 }
  found && /loadtest-quic-incluster.sh await-filed/ { print; exit }
' "$WORKFLOW")"
if [ -n "$filed_step" ]; then
  pass "the workflow waits for the estate to be filed"
else
  fail "the workflow starts its scenarios without waiting for the estate to be filed"
fi

# And it has to ask before the scenarios, not after them. The three run at the
# same time, driven from one step, so that step is the thing to be before.
filed_at="$(grep -n 'await-filed' "$WORKFLOW" || true)"
filed_line="${filed_at%%:*}"
scenarios_at="$(grep -n 'Run the browser-side scenarios against the walk' "$WORKFLOW" || true)"
scenarios_line="${scenarios_at%%:*}"
if [ -n "$filed_line" ] && [ -n "$scenarios_line" ] && [ "$filed_line" -lt "$scenarios_line" ]; then
  pass "the estate is filed before the scenarios read it"
else
  fail "the wait for a filed estate must come before the k6 scenarios"
fi

# The walk the scenarios join is the one the harness is already walking, and the
# harness is the only thing that knows when that started. A generator handed no
# start time begins the profile's shape again from its beginning and is a phase
# behind for the rest of the night.
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
