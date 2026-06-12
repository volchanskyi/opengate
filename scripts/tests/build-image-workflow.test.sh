#!/usr/bin/env bash
# Tests for the SHA-source contract in .github/workflows/build-image.yml.
#
# Bug history: commit 76ead5f wired `HEAD_SHA: ${{ github.event.workflow_run.head_sha || github.sha }}`
# into both the check-image-changed and tag-forward jobs. On a workflow_run
# trigger, workflow_run.head_sha refers to the *triggering* workflow (CI on
# dev), so HEAD_SHA resolved to the dev SHA. CD, however, looks for the main
# SHA (its own workflow_run.head_sha, which references build-image running on
# main). When tag-forward fired, the image was tagged with the dev SHA and CD
# failed with "manifest unknown". See gh run 26130609683.
#
# The fix is to use `github.sha` consistently — on workflow_run-triggered
# runs of build-image, github.sha equals the default branch (main) HEAD,
# matching what docker/metadata-action stamps from in build-and-push and
# what CD subsequently resolves from build-image's own workflow_run.
#
# Run: ./scripts/tests/build-image-workflow.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKFLOW="$SCRIPT_DIR/../../.github/workflows/build-image.yml"

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

echo "build-image-workflow:"

# --- Case 1: no HEAD_SHA expression references workflow_run.head_sha.
if grep -qE '^[[:space:]]*HEAD_SHA:.*workflow_run\.head_sha' "$WORKFLOW"; then
  OFFENDERS="$(grep -nE '^[[:space:]]*HEAD_SHA:.*workflow_run\.head_sha' "$WORKFLOW")"
  fail "HEAD_SHA must not reference workflow_run.head_sha — see header. Offenders: $OFFENDERS"
else
  pass "no HEAD_SHA references workflow_run.head_sha"
fi

# --- Case 2: every HEAD_SHA line references github.sha.
HEAD_SHA_LINES="$(grep -nE '^[[:space:]]*HEAD_SHA:' "$WORKFLOW" || true)"
if [ -z "$HEAD_SHA_LINES" ]; then
  fail "expected at least one HEAD_SHA expression in $WORKFLOW (regressed structure?)"
else
  BAD="$(printf '%s\n' "$HEAD_SHA_LINES" | grep -v 'github\.sha' || true)"
  if [ -z "$BAD" ]; then
    pass "every HEAD_SHA line uses github.sha"
  else
    fail "HEAD_SHA lines not using github.sha: $BAD"
  fi
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
