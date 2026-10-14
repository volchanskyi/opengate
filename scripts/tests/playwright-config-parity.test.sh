#!/usr/bin/env bash
# The staging Playwright config overrides only the base URL, retries and webServer of the local one,
# and narrows the local projects to the specs a deployed release can run.

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

INHERITED_KEYS=(workers globalSetup globalTeardown timeout fullyParallel testDir)
for key in "${INHERITED_KEYS[@]}"; do
  if grep -qE "^[[:space:]]*${key}:" "$STAGING_CFG"; then
    fail "staging config redeclares '${key}' — it must be inherited from playwright.config.ts"
  else
    pass "staging config inherits '${key}'"
  fi
done

# A config's projects are inherited, or built from the local config's own list.
projects_derived() {
  local line
  line="$(grep -E '^[[:space:]]*projects:' "$1" || true)"
  [ -z "$line" ] || grep -qE '^[[:space:]]*projects:[[:space:]]*(shared|local)\.projects' <<<"$line"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
printf '  projects: [\n    { name: "chromium" },\n  ],\n' >"$work/forked.ts"
printf '  projects: shared.projects\n    ?.map((project) => project),\n' >"$work/derived.ts"
if ! projects_derived "$work/forked.ts" && projects_derived "$work/derived.ts"; then
  pass "a projects list written afresh is told apart from one built from the local list"
else
  fail "the projects check cannot tell a forked list from a derived one"
fi
if projects_derived "$STAGING_CFG"; then
  pass "staging config builds its projects from the local config's"
else
  fail "staging config writes its own projects — it must build them from playwright.config.ts's"
fi

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
