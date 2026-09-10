#!/usr/bin/env bash
# Tests for scripts/loadtest-run-budget.sh, and for load-test.yml reading it.
#
# The pod holding the fleet was created with `sleep 1800` and the wait for its
# verdict fixed at 1500 seconds. Neither number appeared anywhere near the hold
# they had to cover, so thirty minutes was a ceiling on every staging run and
# nothing said so — a profile declaring eight hours would have had its pod taken
# away underneath it and reported a harness that reached no verdict, which is
# what a broken cluster looks like too.
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

# The everyday nightly. Both figures are the run's own length plus the room the
# work either side of it takes, rather than a number chosen once and forgotten.
out="$(budget_for 8m)"
assert_eq "an eight-minute hold gives its pod thirty minutes" "1800" "$(field "$out" LOADTEST_POD_LIFETIME_SECONDS)"
assert_eq "an eight-minute hold waits eighteen minutes for a verdict" "1080" "$(field "$out" LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS)"

# The property D28 is about: a longer run gets a longer pod. A fixed sleep is
# the same number whatever the profile asks for, which is how thirty minutes
# came to be a ceiling nobody had declared.
short="$(field "$(budget_for 8m)" LOADTEST_POD_LIFETIME_SECONDS)"
long="$(field "$(budget_for 5h)" LOADTEST_POD_LIFETIME_SECONDS)"
if [ "$long" -gt "$short" ] && [ "$long" -gt 18000 ]; then
  pass "a five-hour hold is given a pod that outlives it"
else
  fail "a five-hour hold is given a pod that outlives it (short=[$short] long=[$long])"
fi

# The pod has to outlive the wait for the verdict, or the bundle is copied out
# of a pod that has already gone.
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

# The whole run fits inside the pod, which is the claim the numbers are making.
out="$(budget_for 5h)"
if [ "$(field "$out" LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS)" -gt 18000 ]; then
  pass "the wait for a verdict covers the hold it is waiting on"
else
  fail "the wait for a verdict covers the hold it is waiting on"
fi

# A hold nobody set is not a hold of zero. The figure everything is derived from
# has to be given, or the derivation is of nothing.
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

# --- The workflow reads it, rather than carrying numbers of its own -----------

workflow="$(cat "$WORKFLOW")"

if grep -q 'loadtest-run-budget.sh' <<<"$workflow"; then
  pass "load-test.yml derives its pod lifetime rather than fixing one"
else
  fail "load-test.yml derives its pod lifetime rather than fixing one"
fi

# The number that started this. A literal sleep beside the pod is the ceiling
# coming back, whatever the derived figure says.
if grep -qE 'sleep","1800"|sleep 1800' <<<"$workflow"; then
  fail "no fixed pod lifetime is left in load-test.yml"
else
  pass "no fixed pod lifetime is left in load-test.yml"
fi

# The job cannot end before the pods it created are meant to. A job timeout
# shorter than the run is the same ceiling wearing different clothes, so this
# reads the hold the workflow actually declares rather than a figure of its own:
# raising the hold has to move the timeout with it, or the check is asking about
# a run nobody makes.
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

# The job holds two terms, not one: the wait on the namespace claim in front of
# this run, and the run it then makes. A timeout covering only the second is a
# run cut off inside its own steady phase, and it reads as a staging pod that
# never went Ready.
#
# It is worth checking because the claim is now renewed for as long as its
# holder works. Before that a long holder's claim went stale and a waiter could
# take it, so a wait too short mostly went unnoticed; now waiting it out is the
# only way past, and the two figures have to add up.
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

# And the walk the profile declares has to fit inside the hold, or machines
# start leaving before the run winds them down and the level drops under the
# phase that is measuring it.
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
