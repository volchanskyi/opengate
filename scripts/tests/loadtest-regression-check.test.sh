#!/usr/bin/env bash
# Offline tests for scripts/loadtest-regression-check.sh.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/loadtest-regression-check.sh"
[ -x "$CHECK" ] || {
  echo "FAIL: $CHECK not executable" >&2
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
  if grep -qF "$needle" <<<"$haystack"; then pass "$name"; else fail "$name (missing [$needle])"; fi
}
assert_not_contains() {
  local name="$1" needle="$2" haystack="$3"
  if grep -qF "$needle" <<<"$haystack"; then fail "$name (unexpected [$needle])"; else pass "$name"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"

cat >"$BIN_DIR/kubectl" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
args="$*"
vec() { printf '{"status":"success","data":{"resultType":"vector","result":[%s]}}\n' "$1"; }
s() { printf '{"metric":{"source":"%s","scenario":"%s","phase":"%s","workload":"%s"},"value":[2000,"%s"]}' "$1" "$2" "$3" "${WL:-w1}" "$4"; }
case "${VM_PROFILE:-seeded}" in
  empty) ;;
  invalid) printf '%s\n' 'not-json' ;;
  seeded)
    if grep -q 'loadtest_p99_advisory_streak' <<<"$args"; then
      # The consecutive-night count the previous run left behind. Empty means
      # no prior sample, which is a series that has never been advisory.
      if [ -n "${STREAK_PRIOR:-}" ]; then
        vec "$(s quic quic-agents connect "$STREAK_PRIOR")"
      fi
    elif grep -q '/api/v1/export' <<<"$args"; then
      # Previous error_rate sample for the exact source/scenario/phase selector.
      printf '%s\n' '{"metric":{"__name__":"loadtest_error_rate","source":"quic","scenario":"quic-agents","phase":"aggregate","commit":"older","env":"ci"},"values":[0.001],"timestamps":[1000]}'
    elif grep -q 'count_over_time' <<<"$args"; then
      vec "$(s quic quic-agents connect 10),$(s quic quic-agents aggregate 10),$(s k6 api-baseline http 10),$(s k6 concurrent-agents http 10)"
    elif grep -q 'loadtest_latency_p95_ms' <<<"$args"; then
      vec "$(s quic quic-agents connect 200),$(s k6 api-baseline http 100)"
    elif grep -q 'loadtest_latency_p50_ms' <<<"$args"; then
      vec "$(s quic quic-agents connect 100),$(s k6 api-baseline http 50)"
    elif grep -q 'loadtest_latency_p99_ms' <<<"$args"; then
      vec "$(s quic quic-agents connect 300),$(s k6 api-baseline http 150)"
    elif grep -q 'loadtest_rps' <<<"$args"; then
      vec "$(s quic quic-agents aggregate 200),$(s k6 concurrent-agents http 30)"
    elif grep -q 'loadtest_error_rate' <<<"$args"; then
      vec "$(s quic quic-agents aggregate 0),$(s k6 api-baseline http 0)"
    fi
    ;;
esac
exit "${KUBECTL_STATUS:-0}"
SH
chmod +x "$BIN_DIR/kubectl"

run_check() {
  local summary="$1"
  (
    export PATH="$BIN_DIR:$PATH"
    export KUBECTL_ARGS="$WORK/kubectl.args"
    export VM_NAMESPACE="observability"
    export VM_SERVICE="private-vm"
    export GITHUB_SHA="deadbeef"
    # The consecutive-night counts land in the work directory rather than
    # wherever the suite happened to be run from. A test that leaves a file in
    # the tree is a test that changes what the next one sees.
    export P99_STREAK_FILE="${P99_STREAK_FILE:-$WORK/streaks.json}"
    "$CHECK" "$summary"
  )
}

write_summary() {
  local file="$1" body="$2"
  printf '%s\n' "$body" >"$file"
}

echo "load-test regression checker:"

write_summary "$WORK/p95-regression.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":1500,"latency_p99_ms":1800,"workload":"w1","commit":"deadbeef","env":"ci"},
  {"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":110,"latency_p99_ms":180,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/p95-regression.json" 2>&1)" || rc=$?
assert_eq "p95 window breach exits 1" "1" "$rc"
assert_contains "p95 regression names breached series" "quic/quic-agents/connect latency_p95_ms" "$out"
assert_contains "p95 regression includes p99 context" "p99=1800" "$out"
assert_not_contains "clean peer series stays out of alert" "k6/api-baseline/http latency_p95_ms" "$out"
assert_contains "window query excludes current commit" 'commit!="deadbeef"' "$(cat "$WORK/kubectl.args")"

write_summary "$WORK/rps-regression.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"aggregate","rps":40,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/rps-regression.json" 2>&1)" || rc=$?
assert_eq "rps drop exits 1" "1" "$rc"
assert_contains "rps alert is direction-aware" "quic/quic-agents/aggregate rps" "$out"

# The staging cluster is shared and free-tier, so a contended night degrades
# throughput and latency together while every agent still succeeds. Those nights
# are environment noise, not product regressions: the tolerance bands are sized
# from the observed run-to-run spread so a night like this stays green, and
# error_rate — which stayed at zero throughout — remains the correctness signal.
write_summary "$WORK/contention-night.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"aggregate","rps":80,"error_rate":0,"workload":"w1","commit":"deadbeef","env":"ci"},
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p50_ms":390,"latency_p95_ms":900,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/contention-night.json" 2>&1)" || rc=$?
assert_eq "shared-cluster contention night stays green" "0" "$rc"
assert_not_contains "contention night raises no regression alert" "REGRESSION_ALERT:" "$out"

write_summary "$WORK/error-rate-regression.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"aggregate","error_rate":0.02,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/error-rate-regression.json" 2>&1)" || rc=$?
assert_eq "error-rate ceiling exits 1" "1" "$rc"
assert_contains "error-rate alert names ceiling" "error_rate" "$out"

write_summary "$WORK/p99-only.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":220,"latency_p99_ms":5000,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/p99-only.json" 2>&1)" || rc=$?
assert_eq "p99-only breach stays green" "0" "$rc"
assert_contains "p99-only breach emits advisory context" "P99_ADVISORY:" "$out"
assert_not_contains "p99-only breach does not emit regression alert" "REGRESSION_ALERT:" "$out"

write_summary "$WORK/cold-start-under-ceiling.json" '[
  {"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":180,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(VM_PROFILE=empty run_check "$WORK/cold-start-under-ceiling.json" 2>&1)" || rc=$?
assert_eq "cold-start under absolute ceiling stays green" "0" "$rc"
assert_not_contains "cold-start under ceiling has no regression alert" "REGRESSION_ALERT:" "$out"

# A cold window means this file has nothing to compare against, and it says so
# by saying nothing. The absolute limits that used to live here are the
# profile's now, read by scripts/loadtest-gate-check.sh, whose own tests cover a
# collapse against them — and that check reads no window at all, so it is
# exactly as awake on the first night of a series as on the hundredth.
#
# The two must not both hold numbers. They did, for the same measurement, with
# different values: 200 in this file and 100 in the profile, one enforced and
# one read by nothing, so an edit to either did not do what it said.
write_summary "$WORK/cold-start-over-ceiling.json" '[
  {"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":250,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(VM_PROFILE=empty run_check "$WORK/cold-start-over-ceiling.json" 2>&1)" || rc=$?
assert_eq "a cold window is compared against nothing here" "0" "$rc"
assert_not_contains "and no alert is invented from a window that does not exist" "REGRESSION_ALERT:" "$out"

# The numbers are gone from this file, in both directions. A copy left behind
# would be the second home this consolidation exists to close.
if grep -qE '^[[:space:]]*(latency_abs_ceiling|p99_abs_ceiling|rps_abs_floor|error_rate_ceiling)\(\)' "$CHECK"; then
  fail "this file still holds absolute limits — the profile is their only home"
else
  pass "this file holds method and no numbers"
fi

write_summary "$WORK/fail-open.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":700,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(KUBECTL_STATUS=19 run_check "$WORK/fail-open.json" 2>&1)" || rc=$?
assert_eq "a store this cannot reach reports nothing rather than guessing" "0" "$rc"
assert_not_contains "transport failure has no regression alert" "REGRESSION_ALERT:" "$out"

write_summary "$WORK/nulls.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":null,"rps":null,"error_rate":null,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/nulls.json" 2>&1)" || rc=$?
assert_eq "null metrics are skipped per series" "0" "$rc"
assert_not_contains "null metrics have no regression alert" "REGRESSION_ALERT:" "$out"

# A series is only comparable to itself.
#
# When a scenario is rewritten to measure something else it keeps its name, so
# the stored numbers and the new ones sit in one series under two different
# pieces of work. That is how a relay scenario that started opening real
# sessions was reported as a collapse against the health check it replaced. The
# workload each sample was produced by travels with it, and the window is keyed
# by that, so a rewritten workload compares against itself or against nothing.
if grep -qF 'by (source, scenario, phase, workload)' "$CHECK"; then
  pass "the window is grouped by the workload that produced each sample"
else
  fail "the window is not grouped by workload — a rewritten scenario still compares against the work it replaced"
fi

write_summary "$WORK/rewritten-workload.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p50_ms":900,"workload":"rewritten","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/rewritten-workload.json" 2>&1)" || rc=$?
assert_eq "a rewritten workload is compared against nothing here" "0" "$rc"
assert_not_contains "the replaced workload's median is not used" "window median" "$out"

# The same, well inside what the profile holds it to. Both nights are silent
# here for the same reason — there is no window — and it is the profile's limits
# that tell them apart.
write_summary "$WORK/rewritten-ok.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p50_ms":300,"workload":"rewritten","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/rewritten-ok.json" 2>&1)" || rc=$?
assert_eq "a new workload with no history passes" "0" "$rc"

# The same figure under the workload the window was built from is still judged
# against that window, so the keying narrows nothing it should not.
write_summary "$WORK/same-workload.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p50_ms":900,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/same-workload.json" 2>&1)" || rc=$?
assert_eq "the established workload is still judged against its window" "1" "$rc"
assert_contains "and by its window median" "window median" "$out"

# --- a repeated advisory escalates instead of absorbing itself ----------------
#
# The slowest one percent of registrations went from 50 ms to 397 ms and three
# nights in a row printed an advisory and returned success. Nothing counted the
# consecutive nights, and the check silences itself: each bad night enters the
# window it is compared against, so the comparison point climbs until a bad night
# is no longer four times anything.
#
# It is not a slow drift. The window is a median over commits, so three nights on
# one commit collapse to one point, and the whole window at the time held five —
# 61.1, 68.8, 85.7, 373.0, 396.9. One more bad night on a new commit takes the
# median from 85.7 to 229.4, the threshold from 343 to 917, and a 390 ms night
# goes quiet with the slowdown recorded as normal.
#
# So the advisory carries how many nights it has been running, read back from the
# same store the run already writes to, and the third one is a finding rather
# than a line in a summary.
# run_streak PRIOR SUMMARY — drives the check with a given prior streak for the
# series the advisory fires on. The two extra names ride into the same runner the
# other cases use, so what differs between a case here and a case above is the
# count that came back and nothing else.
run_streak() {
  local prior="$1" summary="$2"
  STREAK_PRIOR="$prior" P99_STREAK_FILE="$WORK/streaks.json" run_check "$summary"
}

# streak_of SOURCE SCENARIO PHASE — what tonight's run recorded for one series.
streak_of() {
  jq -r --arg s "$1" --arg c "$2" --arg p "$3" \
    '[.[] | select(.source == $s and .scenario == $c and .phase == $p)][0].streak // "none"' \
    "$WORK/streaks.json" 2>/dev/null || printf 'none\n'
}

write_summary "$WORK/p99-streak.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":220,"latency_p99_ms":5000,"workload":"w1","commit":"deadbeef","env":"ci"}
]'

# The first bad night. A finding needs more than one reading, so it reports and
# passes — which is what the advisory has always done.
rm -f "$WORK/streaks.json"
rc=0
out="$(run_streak "" "$WORK/p99-streak.json" 2>&1)" || rc=$?
assert_eq "the first advisory night passes" "0" "$rc"
assert_contains "and prints the advisory" "P99_ADVISORY:" "$out"
assert_eq "and records it as the first night" "1" "$(streak_of quic quic-agents connect)"

# The second. Still reporting, still passing.
rm -f "$WORK/streaks.json"
rc=0
out="$(run_streak "1" "$WORK/p99-streak.json" 2>&1)" || rc=$?
assert_eq "the second advisory night passes" "0" "$rc"
assert_eq "and counts two" "2" "$(streak_of quic quic-agents connect)"
assert_not_contains "and raises nothing yet" "REGRESSION_ALERT:" "$out"

# The third is the finding. It fails the run, which is the only signal that
# reaches a person — the alert path is downstream of the run's own result.
rm -f "$WORK/streaks.json"
rc=0
out="$(run_streak "2" "$WORK/p99-streak.json" 2>&1)" || rc=$?
assert_eq "the third consecutive advisory night fails the run" "1" "$rc"
assert_contains "and raises an alert" "REGRESSION_ALERT:" "$out"
assert_contains "naming the series" "quic/quic-agents/connect" "$out"
assert_contains "and how many nights it has run" "3 consecutive nights" "$out"
assert_eq "and counts three" "3" "$(streak_of quic quic-agents connect)"

# A night that clears resets the count, so three separate bad nights spread over
# a fortnight are not reported as a run of three.
write_summary "$WORK/p99-clear.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":220,"latency_p99_ms":400,"workload":"w1","commit":"deadbeef","env":"ci"}
]'
rm -f "$WORK/streaks.json"
rc=0
out="$(run_streak "2" "$WORK/p99-clear.json" 2>&1)" || rc=$?
assert_eq "a night that clears passes" "0" "$rc"
assert_not_contains "and prints no advisory" "P99_ADVISORY:" "$out"
assert_eq "and puts the count back to nothing" "0" "$(streak_of quic quic-agents connect)"

# The count is read back from the store rather than kept anywhere in the run, so
# the query that reads it is part of what the run does.
assert_contains "the streak is read from the trend store" "loadtest_p99_advisory_streak" "$(cat "$WORK/kubectl.args")"
assert_contains "and the read excludes tonight's own sample" 'commit!="deadbeef"' "$(cat "$WORK/kubectl.args")"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
