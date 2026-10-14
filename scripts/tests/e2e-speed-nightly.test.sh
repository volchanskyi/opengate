#!/usr/bin/env bash
# Keeps the browser speed specs in their own nightly workflow and out of every other run.
# The split is read from Playwright's own listing of each run, so it holds what the config does.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WEB="$ROOT/web"
NIGHTLY="$ROOT/.github/workflows/e2e-performance.yml"
OTHER_RUNS=(
  "$ROOT/.github/workflows/ci.yml"
  "$ROOT/.github/workflows/e2e-cross-browser.yml"
  "$ROOT/Makefile"
  "$ROOT/scripts/precommit-gauntlet.sh"
)

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

echo "e2e-speed-nightly:"

if [ ! -x "$WEB/node_modules/.bin/playwright" ]; then
  printf '  FAIL Playwright is not installed in web/; run: (cd web && npm ci)\n' >&2
  exit 2
fi

# Prints the spec files one Playwright run reaches, one per line, under the variables given.
spec_files() {
  local listing
  if ! listing="$(cd "$WEB" && env -u PLAYWRIGHT_PERFORMANCE -u PLAYWRIGHT_ALL_BROWSERS "$@" \
    node_modules/.bin/playwright test --list --reporter=list 2>&1)"; then
    printf '%s\n' "$listing" >&2
    return 1
  fi
  sed -nE 's/^  \[[^]]+\] [^ ]+ ([^: ]+\.spec\.ts):[0-9]+:[0-9]+ .*/\1/p' <<<"$listing" | sort -u
}

push_specs="$(spec_files)"
cross_specs="$(spec_files PLAYWRIGHT_ALL_BROWSERS=1)"
speed_specs="$(spec_files PLAYWRIGHT_PERFORMANCE=1)"
all_specs="$(cd "$WEB/e2e" && find . -name '*.spec.ts' -printf '%P\n' | sort)"

if [ -n "$speed_specs" ] && [ -n "$push_specs" ]; then
  pass "the speed run reaches $(wc -l <<<"$speed_specs") spec files, the push run $(wc -l <<<"$push_specs")"
else
  fail "a listing reached no spec file, so the split cannot be read"
fi

for spec in $speed_specs; do
  if grep -qxF -- "$spec" <<<"$push_specs"; then
    fail "$spec runs on every push as well as nightly"
  else
    pass "$spec stays out of the push run"
  fi
  if grep -qxF -- "$spec" <<<"$cross_specs"; then
    fail "$spec runs in the cross-browser nightly as well"
  fi
done

lost=""
for spec in $all_specs; do
  grep -qxF -- "$spec" <<<"$push_specs" || grep -qxF -- "$spec" <<<"$speed_specs" || lost="$lost $spec"
done
if [ -z "$lost" ]; then
  pass "every one of the $(wc -l <<<"$all_specs") spec files reaches one of the two runs"
else
  fail "these spec files reach neither run:$lost"
fi

if [ ! -f "$NIGHTLY" ]; then
  fail "missing workflow: $NIGHTLY"
else
  nightly="$(<"$NIGHTLY")"
  for needle in "schedule:" "PLAYWRIGHT_PERFORMANCE: '1'" "e2e-stack-up.sh" "npx playwright test" \
    "e2e-summarize.sh performance"; do
    if grep -qF -- "$needle" <<<"$nightly"; then
      pass "the nightly workflow carries $needle"
    else
      fail "the nightly workflow lacks $needle"
    fi
  done
fi

for entry in "${OTHER_RUNS[@]}"; do
  if grep -qF 'PLAYWRIGHT_PERFORMANCE' "$entry"; then
    fail "$(basename "$entry") switches the speed run on"
  else
    pass "$(basename "$entry") leaves the speed run off"
  fi
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '\nFailures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
