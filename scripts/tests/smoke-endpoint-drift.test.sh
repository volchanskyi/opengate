#!/usr/bin/env bash
# Guards the deploy smoke test against API-path drift.
#
# deploy/scripts/smoke-test.sh hand-writes the API paths it probes, and it runs
# in exactly one place: cd.yml, after a merge to main, against staging and
# production. Nothing before that point reads those URL strings — not the
# OpenAPI spec-drift check (it scans the spec for unguarded mutating ops), not
# check-doc-links (Markdown links under docs/ and .claude/), not the Go or web
# suites. So renaming a route in api/openapi.yaml leaves the smoke test probing
# a path that no longer exists, and the first thing that notices is a failed
# staging deploy — which is how the /api/v1/groups -> /api/v1/sites rename broke
# CD.
#
# Fix by construction: api/openapi.yaml is the single source of truth. Every
# OpenGate API path the smoke test probes must be declared there.
#
# Run: ./scripts/tests/smoke-endpoint-drift.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SPEC="$ROOT/api/openapi.yaml"
SMOKE="$ROOT/deploy/scripts/smoke-test.sh"

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
summarize() {
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  if [ "$FAIL" -gt 0 ]; then
    printf '  - %s\n' "${FAILURES[@]}" >&2
    exit 1
  fi
  exit 0
}

echo "smoke-endpoint-drift:"

for f in "$SPEC" "$SMOKE"; do
  if [ ! -f "$f" ]; then
    echo "FAIL: $f not found" >&2
    exit 1
  fi
done

# Paths the spec declares, as two-space-indented top-level keys, with each
# {param} name flattened so a placeholder compares by position, not by spelling.
declared="$(grep -oE '^  (/[^ :]+):' "$SPEC" | tr -d ' :' \
  | awk '{ gsub(/[{][A-Za-z0-9_]*[}]/, "{param}"); print }' | sort -u)"
if [ -z "$declared" ]; then
  fail "api/openapi.yaml declares at least one path (nothing matched — a grep that finds nothing is a failure, not a pass)"
  summarize
fi
pass "api/openapi.yaml declares $(wc -l <<<"$declared") paths"

# Paths the smoke test probes. Shell expansions collapse to the same {param}
# placeholder, so a probe built from a variable compares against the templated
# declaration. Comments are dropped: a path only counts when it is requested.
probed="$(grep -v '^[[:space:]]*#' "$SMOKE" \
  | awk '{ gsub(/[$][{]?[A-Za-z_][A-Za-z0-9_]*[}]?/, "{param}"); print }' \
  | grep -oE '/api/v1/[-A-Za-z0-9_./{}]*' \
  | awk '{ sub(/\/$/, ""); print }' | sort -u)"
if [ -z "$probed" ]; then
  fail "deploy/scripts/smoke-test.sh probes at least one /api/v1 path (extraction matched nothing — the check would pass vacuously)"
  summarize
fi

drift=0
while IFS= read -r path; do
  [ -n "$path" ] || continue
  if grep -qxF "$path" <<<"$declared"; then
    pass "smoke test probes $path, declared in the spec"
  else
    drift=1
    fail "smoke test probes $path, which api/openapi.yaml does not declare"
  fi
done <<<"$probed"

if [ "$drift" -eq 0 ]; then
  pass "no API-path drift between the smoke test and the spec"
fi

summarize
