#!/usr/bin/env bash
# Pins the staging Playwright config to the local one.
#
# The two configs run the SAME spec files against different targets, so any
# execution setting that differs makes the local run a weaker predictor of the
# staging run. That gap is not theoretical: the local config pins `workers: 1`
# with a comment explaining that parallel workers contend on shared server-side
# IAM state, while a forked staging config silently kept Playwright's default
# worker count.
#
# The rule this gate encodes: the staging config DERIVES from the local one and
# overrides only what is genuinely target-specific (base URL, retries, and the
# absence of a webServer to bring up). Anything that changes how the suite
# executes must be declared once, in the local config, and inherited.

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

# 1. The staging config must import the local one rather than restate it.
if grep -qE '^import .* from "\./playwright\.config"' "$STAGING_CFG"; then
  pass "staging config imports ./playwright.config"
else
  fail "staging config does not import ./playwright.config — it must derive from it, not fork it"
fi

# 2. Settings that govern how the suite EXECUTES belong to the local config
#    alone. A staging-side redeclaration is drift by construction.
INHERITED_KEYS=(workers globalSetup globalTeardown projects timeout fullyParallel testDir)
for key in "${INHERITED_KEYS[@]}"; do
  if grep -qE "^[[:space:]]*${key}:" "$STAGING_CFG"; then
    fail "staging config redeclares '${key}' — it must be inherited from playwright.config.ts"
  else
    pass "staging config inherits '${key}'"
  fi
done

# 3. The local config must actually pin the serialization the staging run now
#    inherits. If this pin is ever dropped, both runs silently go parallel.
if grep -qE '^[[:space:]]*workers:[[:space:]]*1,' "$LOCAL_CFG"; then
  pass "local config pins workers: 1"
else
  fail "local config no longer pins 'workers: 1' — staging inherits it, so both runs would go parallel"
fi

# 4. The staging run targets the port-forward, not the docker-compose stack.
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
