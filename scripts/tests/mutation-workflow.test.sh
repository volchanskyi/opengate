#!/usr/bin/env bash
# Tests for the mutation workflow's timeout and publish failure classification.
#
# Bug history: GitHub Actions run 27743482464 cancelled the Go gremlins leg at
# the 90-minute job cap before server/mutation-report.json could be uploaded.
# The publish job then collapsed mutation-summarize.sh exit 2 (missing input)
# into "regression=1", mislabeling an incomplete run as a mutation score
# regression. Keep the timeout intentional and preserve exit-code semantics:
#   0 = clean, 1 = score regression, 2 = incomplete/malformed input.
#
# Run: ./scripts/tests/mutation-workflow.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKFLOW="$SCRIPT_DIR/../../.github/workflows/mutation.yml"

if [ ! -f "$WORKFLOW" ]; then
  echo "FAIL: $WORKFLOW not found" >&2
  exit 1
fi

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

echo "mutation-workflow:"

if grep -qE '^[[:space:]]*timeout-minutes:[[:space:]]*100([[:space:]]|$)' "$WORKFLOW"; then
  pass "mutation matrix timeout is 100 minutes"
else
  fail "mutation matrix timeout must be 100 minutes"
fi

if grep -q 'SUMMARY_STATUS=' "$WORKFLOW" \
  && grep -q 'Mutation summary input missing or invalid' "$WORKFLOW" \
  && grep -qE '^[[:space:]]*2\)' "$WORKFLOW"; then
  pass "summarize exit 2 is classified as incomplete input"
else
  fail "summarize exit 2 must fail as incomplete input, not regression"
fi

if grep -qE '^[[:space:]]*0\)[[:space:]]*REGRESSION=0[[:space:]]*;;' "$WORKFLOW" \
  && grep -qE '^[[:space:]]*1\)[[:space:]]*REGRESSION=1[[:space:]]*;;' "$WORKFLOW"; then
  pass "summarize exit 0/1 preserve clean/regression semantics"
else
  fail "summarize exit 0/1 must preserve clean/regression semantics"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
