#!/usr/bin/env bash
# Guards the JS budget against being cleared by exclusion rather than by weight.
#
# The whole-app budget is a glob with holes in it: a vendor engine that loads on
# one lazy route is pulled into a named chunk and subtracted from the app total,
# so a regression in the routes is not hidden under one dependency's size. That
# is only honest while every subtracted chunk carries a budget of its own.
# Without this check the cheapest way to pass the gate is to name a chunk and
# take it out of the glob, which is the same as deleting the budget for it.
#
# So: every chunk vite splits out by name is budgeted, and every hole in the
# app-total glob names a chunk that is. Neither list may grow without the other.
#
# Run: ./scripts/tests/bundle-budget-coverage.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
VITE="$ROOT/web/vite.config.ts"
BUDGET="$ROOT/web/.size-limit.json"

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

echo "bundle-budget-coverage:"

for f in "$VITE" "$BUDGET"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

if ! jq empty "$BUDGET" 2>/dev/null; then
  fail "$BUDGET is not valid JSON"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

# Chunk names vite splits out by hand: the string each manualChunks branch
# returns. Comment lines are stripped so the prose explaining a split does not
# read as one.
named_chunks="$(
  sed -e 's#//.*##' "$VITE" \
    | grep -oE "return '[a-z0-9-]+'" \
    | sed -E "s/return '([a-z0-9-]+)'/\1/" | sort -u
)"

# Chunks the app-total entry subtracts from its glob: the `!…/<name>-*.js` holes.
excluded="$(
  jq -r '.[] | select((.path | type) == "array") | .path[] | select(startswith("!"))' "$BUDGET" \
    | sed -E 's#^!dist/assets/([a-z0-9-]+)-\*\.js$#\1#' | sort -u
)"

# Chunks that carry a budget of their own: an entry whose single path names one.
budgeted="$(
  jq -r '.[] | select((.path | type) == "string") | .path' "$BUDGET" \
    | sed -nE 's#^dist/assets/([a-z0-9-]+)-\*\.js$#\1#p' | sort -u
)"

if [ -z "$named_chunks" ]; then
  fail "no manual chunk names found in web/vite.config.ts — the parser has drifted"
else
  pass "found manual chunks: $(echo "$named_chunks" | tr '\n' ' ')"
fi

for chunk in $named_chunks; do
  if printf '%s\n' "$budgeted" | grep -qxF "$chunk"; then
    pass "the $chunk chunk carries its own budget"
  else
    fail "the $chunk chunk is split out but budgeted by nothing"
  fi
  if printf '%s\n' "$excluded" | grep -qxF "$chunk"; then
    pass "the $chunk chunk is subtracted from the whole-app total"
  else
    fail "the $chunk chunk is budgeted separately but still inside the app total — it would be counted twice"
  fi
done

for chunk in $excluded; do
  if printf '%s\n' "$named_chunks" | grep -qxF "$chunk"; then
    continue
  fi
  fail "the app total subtracts $chunk, which vite never splits out"
done
if [ -n "$excluded" ]; then
  pass "every hole in the app-total glob names a chunk vite actually creates"
fi

# A budget with no number is not a budget.
missing_limit="$(jq -r '.[] | select(has("limit") | not) | .name' "$BUDGET")"
if [ -z "$missing_limit" ]; then
  pass "every budget entry states a limit"
else
  fail "budget entries with no limit: $(echo "$missing_limit" | tr '\n' ' ')"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
