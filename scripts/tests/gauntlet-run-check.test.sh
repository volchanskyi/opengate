#!/usr/bin/env bash
# Holds the gauntlet's run_check to naming the exit code a failed check's command returned.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GAUNTLET="$REPO_ROOT/scripts/precommit-gauntlet.sh"

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

# The shipped run_check, read from the gauntlet itself; the steps it reports through print a word.
sed -n '/^run_check() {/,/^}/p' "$GAUNTLET" >"$WORK/run-check.sh"
[ -s "$WORK/run-check.sh" ] || {
  echo "FAIL: no run_check in $GAUNTLET" >&2
  exit 1
}

run() {
  TMPDIR="$WORK" bash -c '
    set -euo pipefail
    running() { :; }
    ok() { echo "ok $1"; }
    fail() { echo "fail $1"; }
    step_clock() { echo 0; }
    FAIL_COUNT=0
    FAILED_STEPS=()
    . "$1"
    shift
    run_check "$@"
    echo "count=$FAIL_COUNT steps=${FAILED_STEPS[*]:-}"
  ' _ "$WORK/run-check.sh" "$@" 2>&1
}

out="$(run "exits three" -- bash -c 'echo broken; exit 3')"
if grep -qF 'exit code: 3)' <<<"$out"; then
  pass "a failed check names the exit code its command returned"
else
  fail "a failed check misreports its exit code: $out"
fi
if grep -qF 'count=1 steps=exits three' <<<"$out" && grep -qF 'broken' <<<"$out"; then
  pass "a failed check is counted, named and shows its output"
else
  fail "a failed check was not counted, named and shown: $out"
fi

out="$(run "passes" -- true)"
if grep -qxF 'ok passes' <<<"$out" && grep -qF 'count=0 steps=' <<<"$out"; then
  pass "a passing check counts no failure"
else
  fail "a passing check was reported as: $out"
fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
