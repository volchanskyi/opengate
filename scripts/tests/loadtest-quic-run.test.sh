#!/usr/bin/env bash
# Tests for scripts/loadtest-quic-run.sh — the QUIC half's keep-or-discard rule.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/loadtest-quic-run.sh"
[ -x "$RUNNER" ] || {
  echo "FAIL: $RUNNER not executable" >&2
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
assert_file() {
  local name="$1" path="$2"
  if [ -f "$path" ]; then pass "$name"; else fail "$name (missing $path)"; fi
}
assert_no_file() {
  local name="$1" path="$2"
  if [ -f "$path" ]; then fail "$name (present $path)"; else pass "$name"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A harness stand-in: prints what the case needs, exits with the code it needs.
make_harness() {
  local exit_code="$1" body="$2"
  cat >"$WORK/harness" <<EOF
#!/usr/bin/env bash
cat <<'BODY'
$body
BODY
exit $exit_code
EOF
  chmod +x "$WORK/harness"
}

COMPLETE_RUN='Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  4.9s
Agents:      100/100 succeeded
Failures:    0

Connect:     p50=10ms  p95=40ms  p99=60ms
Handshake:   p50=20ms  p95=40ms  p99=60ms
Register:    p50=5ms  p95=10ms  p99=15ms'

PARTIAL_RUN='Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  9.1s
Agents:      62/100 succeeded
Failures:    38

Connect:     p50=90ms  p95=800ms  p99=1.5s
Handshake:   p50=20ms  p95=40ms  p99=60ms
Register:    p50=5ms  p95=10ms  p99=15ms'

ABORTED='Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090
2026/08/21 02:00:00 cert manager: open /tmp/loadtest-data/ca.crt: no such file'

run_case() {
  local exit_code="$1" body="$2"
  make_harness "$exit_code" "$body"
  rm -f "$WORK/quic.txt" "$WORK/quic.txt.status"
  STATUS=0
  "$RUNNER" "$WORK/quic.txt" -- "$WORK/harness" >/dev/null 2>"$WORK/err.txt" || STATUS=$?
}

echo "loadtest-quic-run:"

# A clean run measured the fleet.
run_case 0 "$COMPLETE_RUN"
assert_eq "clean run exits 0" "0" "$STATUS"
assert_file "clean run keeps its output" "$WORK/quic.txt"

# A fleet that half connected is a measurement — the error rate is the finding —
# so the exit status survives and the output is kept.
run_case 1 "$PARTIAL_RUN"
assert_eq "agent failures propagate 1" "1" "$STATUS"
assert_file "agent failures keep the output" "$WORK/quic.txt"

# A harness that could not start describes its own failure, not the server's
# latency. Trending zeroes from it drags the window median down for every later
# run, so the output is discarded.
run_case 2 "$ABORTED"
assert_eq "an aborted harness propagates its status" "2" "$STATUS"
assert_no_file "an aborted harness discards its output" "$WORK/quic.txt"
if grep -q "discarded" "$WORK/err.txt"; then
  pass "an aborted harness says its output was discarded"
else
  fail "an aborted harness says its output was discarded"
fi

# A run that finished and connected nobody is the third outcome, and it is not
# an abort: the harness ran to completion and printed a full results block, and
# what it measured was nothing. Its rows are zeroes, so they are discarded the
# same way — but a message calling it an abort sends a reader looking for a crash
# that never happened.
MEASURED_NOTHING='Starting QUIC load test: 500 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  6m12.2s
Agents:      0/500 succeeded
Failures:    0

::error::scenario "quic-agents" produced no rows, so this run is a partial night rather than a measurement'

run_case 2 "$MEASURED_NOTHING"
assert_eq "a run that measured nothing propagates its status" "2" "$STATUS"
assert_no_file "a run that measured nothing discards its output" "$WORK/quic.txt"
if grep -q "measured nothing" "$WORK/err.txt"; then
  pass "a run that measured nothing is named as one, not as an abort"
else
  fail "a run that measured nothing is named as one, not as an abort"
fi

# The exit code is not the only thing that can lie. Output with no results block
# never completed a run, whatever it exited with.
run_case 0 "$ABORTED"
if [ "$STATUS" -ne 0 ]; then
  pass "output with no results block fails even on a zero exit"
else
  fail "output with no results block fails even on a zero exit"
fi
assert_no_file "output with no results block is discarded" "$WORK/quic.txt"

# A results block whose agent line is malformed cannot be summarized, so it is
# not a measurement either.
run_case 0 '=== Results ===
Total time:  4.9s
Agents:      lots succeeded'
if [ "$STATUS" -ne 0 ]; then
  pass "a malformed agent line is not a measurement"
else
  fail "a malformed agent line is not a measurement"
fi

# The verdict is the runner's own exit code, and the caller waits on it, so
# there is nothing to leave beside the output for somebody else to read.
run_case 0 "$COMPLETE_RUN"
assert_no_file "a clean run leaves no verdict file beside its output" "$WORK/quic.txt.status"
run_case 2 "$ABORTED"
assert_no_file "an aborted harness leaves no verdict file beside its output" "$WORK/quic.txt.status"

# Usage errors are refused rather than silently running nothing.
STATUS=0
"$RUNNER" >/dev/null 2>&1 || STATUS=$?
assert_eq "no arguments exits 2" "2" "$STATUS"
STATUS=0
"$RUNNER" "$WORK/quic.txt" "$WORK/harness" >/dev/null 2>&1 || STATUS=$?
assert_eq "a missing -- separator exits 2" "2" "$STATUS"

# The workflow must drive the QUIC harness through this runner, or the half that
# has no keep-or-discard rule is the half that runs.
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
wrapped="$(grep -cE 'scripts/loadtest-quic-run\.sh' "$WORKFLOW" || true)"
if [ "$wrapped" -ge 1 ]; then
  pass "load-test.yml runs the QUIC harness through the runner"
else
  fail "load-test.yml runs the QUIC harness through the runner"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
