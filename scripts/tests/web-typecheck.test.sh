#!/usr/bin/env bash
# The gauntlet type-checks the web tree with `tsc -b` among its lints, ahead of the tests and e2e.
# web/tsconfig.json is a solution file, so `tsc --noEmit` there checks no source at all.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GAUNTLET="$ROOT/scripts/precommit-gauntlet.sh"

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

line_of() { # first line number matching the fixed string, or 0
  local hit
  hit="$(grep -nF -m1 -- "$1" "$GAUNTLET" || true)"
  hit="${hit%%:*}"
  echo "${hit:-0}"
}

echo "web-typecheck:"

check="$(line_of "cd web && npx tsc -b")"
lints="$(line_of 'banner "Lints"')"
codegen="$(line_of 'banner "Codegen sync"')"
e2e="$(line_of 'make e2e')"

if [ "$check" -gt 0 ]; then
  pass "the gauntlet runs tsc -b in web/"
else
  fail "the gauntlet runs tsc -b in web/"
fi
if [ "$lints" -gt 0 ] && [ "$codegen" -gt 0 ] && [ "$check" -gt "$lints" ] && [ "$check" -lt "$codegen" ]; then
  pass "among the lints, before codegen, the tests and e2e"
else
  fail "among the lints, before codegen, the tests and e2e (lints=$lints check=$check codegen=$codegen)"
fi
if [ "$check" -gt 0 ] && [ "$e2e" -gt 0 ] && [ "$check" -lt "$e2e" ]; then
  pass "well ahead of e2e, whose image build is the other place the web tree is type-checked"
else
  fail "well ahead of e2e (check=$check e2e=$e2e)"
fi
if grep -qE 'tsc[^|;&]*--noEmit' "$GAUNTLET"; then
  fail "the gauntlet never type-checks web with tsc --noEmit"
else
  pass "the gauntlet never type-checks web with tsc --noEmit"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
