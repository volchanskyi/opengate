#!/usr/bin/env bash
# The staging Playwright config overrides only the base URL, retries and webServer of the local one.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOCAL_CFG="$REPO_ROOT/web/playwright.config.ts"
STAGING_CFG="$REPO_ROOT/web/playwright.staging.config.ts"

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

echo "playwright config parity:"

for f in "$LOCAL_CFG" "$STAGING_CFG"; do
  if [ ! -f "$f" ]; then
    printf '  FAIL missing config: %s\n' "$f" >&2
    exit 1
  fi
done

if grep -qE '^import .* from "\./playwright\.config"' "$STAGING_CFG"; then
  pass "staging config imports ./playwright.config"
else
  fail "staging config does not import ./playwright.config — it must derive from it, not fork it"
fi

INHERITED_KEYS=(workers globalSetup globalTeardown projects timeout fullyParallel testDir)
for key in "${INHERITED_KEYS[@]}"; do
  if grep -qE "^[[:space:]]*${key}:" "$STAGING_CFG"; then
    fail "staging config redeclares '${key}' — it must be inherited from playwright.config.ts"
  else
    pass "staging config inherits '${key}'"
  fi
done

if grep -qE '^[[:space:]]*workers:[[:space:]]*1,' "$LOCAL_CFG"; then
  pass "local config pins workers: 1"
else
  fail "local config no longer pins 'workers: 1' — staging inherits it, so both runs would go parallel"
fi

if grep -qE 'baseURL:[[:space:]]*"http://127\.0\.0\.1:18080"' "$STAGING_CFG"; then
  pass "staging config targets the staging port-forward"
else
  fail "staging config does not target http://127.0.0.1:18080"
fi

printf '\n  %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '\nFailures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
