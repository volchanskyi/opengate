#!/usr/bin/env bash
# Tests for scripts/loadtest-run-budget.sh, and for load-test.yml reading it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BUDGET="$REPO_ROOT/scripts/loadtest-run-budget.sh"
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"

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

echo "loadtest-run-budget:"

[ -x "$BUDGET" ] || {
  echo "FAIL: $BUDGET not executable" >&2
  exit 1
}

budget_for() { LOADTEST_HOLD="$1" "$BUDGET"; }
field() { sed -n "s/^$2=\(.*\)$/\1/p" <<<"$1"; }

out="$(budget_for 8m)"
assert_eq "an eight-minute hold gives its pod thirty minutes" "1800" "$(field "$out" LOADTEST_POD_LIFETIME_SECONDS)"
assert_eq "an eight-minute hold waits eighteen minutes for a verdict" "1080" "$(field "$out" LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS)"

short="$(field "$(budget_for 8m)" LOADTEST_POD_LIFETIME_SECONDS)"
long="$(field "$(budget_for 5h)" LOADTEST_POD_LIFETIME_SECONDS)"
if [ "$long" -gt "$short" ] && [ "$long" -gt 18000 ]; then
  pass "a five-hour hold is given a pod that outlives it"
else
  fail "a five-hour hold is given a pod that outlives it (short=[$short] long=[$long])"
fi

# The pod outlives the wait for the verdict so the bundle is copied out of a live pod.
for hold in 90s 8m 5h 2h30m; do
  out="$(budget_for "$hold")"
  life="$(field "$out" LOADTEST_POD_LIFETIME_SECONDS)"
  wait_for="$(field "$out" LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS)"
  if [ "$life" -gt "$wait_for" ]; then
    pass "a $hold hold keeps its pod past the verdict"
  else
    fail "a $hold hold keeps its pod past the verdict (pod=[$life] verdict=[$wait_for])"
  fi
done

out="$(budget_for 5h)"
if [ "$(field "$out" LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS)" -gt 18000 ]; then
  pass "the wait for a verdict covers the hold it is waiting on"
else
  fail "the wait for a verdict covers the hold it is waiting on"
fi

if out="$(LOADTEST_HOLD='' "$BUDGET" 2>&1)"; then
  fail "a hold nobody declared is refused"
else
  pass "a hold nobody declared is refused"
fi

if out="$(LOADTEST_HOLD=nonsense "$BUDGET" 2>&1)"; then
  fail "a hold the harness would not accept is refused"
elif grep -q 'nonsense' <<<"$out"; then
  pass "a hold the harness would not accept is refused"
else
  fail "a hold the harness would not accept is refused (got=[$out])"
fi

if out="$(LOADTEST_HOLD=0s "$BUDGET" 2>&1)"; then
  fail "a hold of nothing is refused"
else
  pass "a hold of nothing is refused"
fi

workflow="$(cat "$WORKFLOW")"

if grep -q 'loadtest-run-budget.sh' <<<"$workflow"; then
  pass "load-test.yml derives its pod lifetime rather than fixing one"
else
  fail "load-test.yml derives its pod lifetime rather than fixing one"
fi

if grep -qE 'sleep","1800"|sleep 1800' <<<"$workflow"; then
  fail "no fixed pod lifetime is left in load-test.yml"
else
  pass "no fixed pod lifetime is left in load-test.yml"
fi

# The check reads the hold the workflow declares, so raising the hold moves the timeout bound.
declared_hold="$(sed -n 's/^  LOADTEST_HOLD: \(.*\)$/\1/p' <<<"$workflow" | head -1)"
if [ -n "$declared_hold" ]; then
  pass "load-test.yml declares the hold everything is derived from"
else
  fail "load-test.yml declares the hold everything is derived from"
fi

job_timeout="$(sed -n 's/^    timeout-minutes: \(.*\)$/\1/p' <<<"$workflow" | head -1)"
pod_minutes=$(($(field "$(budget_for "${declared_hold:-8m}")" LOADTEST_POD_LIFETIME_SECONDS) / 60))
if [ -n "$job_timeout" ] && [ "$job_timeout" -gt "$pod_minutes" ]; then
  pass "the job outlives the pods it creates (hold=$declared_hold)"
else
  fail "the job outlives the pods it creates (job=[${job_timeout}m] pods=[${pod_minutes}m] hold=[$declared_hold])"
fi

# The job timeout covers two terms: the wait on the namespace claim and the run itself.
lease_wait="$(sed -n 's/^ *STAGING_LEASE_WAIT_SECONDS: "\([0-9]*\)"$/\1/p' <<<"$workflow" | head -1)"
if [ -n "$lease_wait" ]; then
  pass "load-test.yml declares how long it waits for the namespace"
else
  fail "load-test.yml declares how long it waits for the namespace"
fi

run_seconds="$(field "$(budget_for "${declared_hold:-8m}")" LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS)"
needed_minutes=$(((${lease_wait:-0} + run_seconds) / 60))
if [ -n "$job_timeout" ] && [ "$job_timeout" -ge "$needed_minutes" ]; then
  pass "the job covers the wait and the run together (${needed_minutes}m of ${job_timeout}m)"
else
  fail "the job covers the wait and the run together (needs ${needed_minutes}m, has ${job_timeout}m)"
fi

# The walk the profile declares fits inside the hold, so machines leave only when the run ends.
profile_minutes="$(sed -n 's/^ *duration: \([0-9]*\)m$/\1/p' "$REPO_ROOT/load/profiles/normal.yaml" | awk '{ t += $1 } END { print t + 1 }')"
hold_minutes=$(($(field "$(budget_for "${declared_hold:-8m}")" LOADTEST_HOLD_SECONDS) / 60))
if [ "$hold_minutes" -gt "$profile_minutes" ]; then
  pass "the hold covers the walk the profile declares (~${profile_minutes}m)"
else
  fail "the hold covers the walk the profile declares (hold=[${hold_minutes}m] walk=[~${profile_minutes}m])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
