#!/usr/bin/env bash
# Tests for scripts/assert-mutation-report.sh — the shard's own read-back of the
# report its mutation tool was supposed to write.
#
# The tool step swallows the tool's exit code on purpose (a surviving mutant is
# not a build failure), so the report is the only thing left that says the shard
# did any work. These tests pin what the guard treats as work: a readable report
# with content, and nothing else.

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

# run_guard ARGS… — runs the guard and prints "<exit>|<stderr+stdout>".
run_guard() {
  local out status=0
  out="$("$GUARD" "$@" 2>&1)" || status=$?
  printf '%s|%s' "$status" "$out"
}

# --- a report with content is the shard's evidence of work ---------------------
printf '%s' '{"mutants_killed":10,"mutants_lived":1}' >"$WORK/report.json"
result="$(run_guard gremlins "$WORK/report.json")"
if [ "${result%%|*}" = "0" ]; then
  pass "a report with content passes"
else
  fail "a report with content must pass (got: $result)"
fi

# --- an absent report is a shard that did nothing ------------------------------
result="$(run_guard gremlins "$WORK/absent.json")"
if [ "${result%%|*}" != "0" ]; then
  pass "an absent report fails the shard"
else
  fail "an absent report must fail the shard"
fi
if printf '%s' "${result#*|}" | grep -q 'gremlins'; then
  pass "the failure names the tool that was refused"
else
  fail "the failure must name the tool (got: ${result#*|})"
fi

# --- an empty report is not a report -------------------------------------------
: >"$WORK/empty.json"
result="$(run_guard gremlins "$WORK/empty.json")"
if [ "${result%%|*}" != "0" ]; then
  pass "an empty report fails the shard"
else
  fail "an empty report must fail the shard"
fi

# --- a directory of outcomes is accepted by naming the file inside it -----------
mkdir -p "$WORK/mutants.out"
printf '%s' '{"outcomes":[]}' >"$WORK/mutants.out/outcomes.json"
result="$(run_guard cargo-mutants "$WORK/mutants.out/outcomes.json")"
if [ "${result%%|*}" = "0" ]; then
  pass "cargo-mutants' outcomes.json passes"
else
  fail "cargo-mutants' outcomes.json must pass (got: $result)"
fi

# --- the guard refuses to answer without being told what to look for ------------
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
