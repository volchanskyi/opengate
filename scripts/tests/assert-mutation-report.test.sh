#!/usr/bin/env bash
# Tests for scripts/assert-mutation-report.sh: only a readable report with content counts as work.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GUARD="$ROOT/scripts/assert-mutation-report.sh"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "assert-mutation-report:"

if [ -x "$GUARD" ]; then
  pass "the guard is executable"
else
  fail "the guard must be executable ($GUARD)"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

run_guard() {
  local out status=0
  out="$("$GUARD" "$@" 2>&1)" || status=$?
  printf '%s|%s' "$status" "$out"
}

printf '%s' '{"mutants_killed":10,"mutants_lived":1}' >"$WORK/report.json"
result="$(run_guard gremlins "$WORK/report.json")"
if [ "${result%%|*}" = "0" ]; then
  pass "a report with content passes"
else
  fail "a report with content must pass (got: $result)"
fi

result="$(run_guard gremlins "$WORK/absent.json")"
if [ "${result%%|*}" != "0" ]; then
  pass "an absent report fails the shard"
else
  fail "an absent report must fail the shard"
fi
if grep -qF 'gremlins' <<<"${result#*|}"; then
  pass "the failure names the tool that was refused"
else
  fail "the failure must name the tool (got: ${result#*|})"
fi

: >"$WORK/empty.json"
result="$(run_guard gremlins "$WORK/empty.json")"
if [ "${result%%|*}" != "0" ]; then
  pass "an empty report fails the shard"
else
  fail "an empty report must fail the shard"
fi

mkdir -p "$WORK/mutants.out"
printf '%s' '{"outcomes":[]}' >"$WORK/mutants.out/outcomes.json"
result="$(run_guard cargo-mutants "$WORK/mutants.out/outcomes.json")"
if [ "${result%%|*}" = "0" ]; then
  pass "cargo-mutants' outcomes.json passes"
else
  fail "cargo-mutants' outcomes.json must pass (got: $result)"
fi

# A runner whose per-test filter matches nothing runs zero tests and reports every mutant survived.
cat >"$WORK/stryker-ran-nothing.json" <<'REPORT'
{"files":{"src/a.ts":{"mutants":[
  {"id":"1","status":"Survived","coveredBy":["t1","t2"],"testsCompleted":0},
  {"id":"2","status":"NoCoverage","coveredBy":[],"testsCompleted":0}
]}}}
REPORT
result="$(run_guard stryker "$WORK/stryker-ran-nothing.json")"
if [ "${result%%|*}" != "0" ]; then
  pass "a Stryker report whose covered mutants ran no tests fails the shard"
else
  fail "a Stryker report whose covered mutants ran no tests must fail the shard"
fi
if grep -qF 'ran no tests' <<<"${result#*|}"; then
  pass "and says the tests never ran, rather than that they were weak"
else
  fail "and must say the tests never ran (got: ${result#*|})"
fi

cat >"$WORK/stryker-ran.json" <<'REPORT'
{"files":{"src/a.ts":{"mutants":[
  {"id":"1","status":"Survived","coveredBy":["t1"],"testsCompleted":1},
  {"id":"2","status":"Killed","coveredBy":["t1"],"testsCompleted":1},
  {"id":"3","status":"NoCoverage","coveredBy":[],"testsCompleted":0}
]}}}
REPORT
result="$(run_guard stryker "$WORK/stryker-ran.json")"
if [ "${result%%|*}" = "0" ]; then
  pass "a Stryker report whose surviving mutants were tested passes"
else
  fail "a Stryker report whose surviving mutants were tested must pass (got: $result)"
fi

result="$(run_guard)"
if [ "${result%%|*}" = "2" ]; then
  pass "a call naming no report is a usage error, not a pass"
else
  fail "a call naming no report must exit 2 (got: $result)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
