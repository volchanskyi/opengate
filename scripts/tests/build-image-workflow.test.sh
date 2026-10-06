#!/usr/bin/env bash
# Tests for the SHA-source contract in .github/workflows/build-image.yml.
# HEAD_SHA is github.sha, which on a workflow_run trigger is the main HEAD that CD resolves.

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

if grep -qE '^[[:space:]]*HEAD_SHA:.*workflow_run\.head_sha' "$WORKFLOW"; then
  OFFENDERS="$(grep -nE '^[[:space:]]*HEAD_SHA:.*workflow_run\.head_sha' "$WORKFLOW")"
  fail "HEAD_SHA must not reference workflow_run.head_sha — see header. Offenders: $OFFENDERS"
else
  pass "no HEAD_SHA references workflow_run.head_sha"
fi

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

# The deploy token only reads, so this workflow builds the agent binary and the deploy downloads it.

job_body() {
  awk -v want="  $1:" '
    $0 == want { in_job = 1; next }
    in_job && /^  [a-z]/ { exit }
    in_job { print }
  ' "$WORKFLOW"
}

AGENT_JOB="$(job_body build-agent)"
if [ -n "$AGENT_JOB" ]; then
  pass "build-agent job exists"
else
  fail "build-agent job exists"
fi

for target in x86_64-unknown-linux-musl aarch64-unknown-linux-musl; do
  if grep -qF "$target" <<<"$AGENT_JOB"; then
    pass "build-agent builds $target"
  else
    fail "build-agent does not build $target — the staging node may be either architecture"
  fi
done

# The deploy needs the binary on every run, including those on the tag-forward path.
if grep -qF 'image_changed' <<<"$AGENT_JOB"; then
  fail "build-agent is gated on image_changed, so the tag-forward path leaves the deploy with no binary"
else
  pass "build-agent is not gated on image_changed"
fi

if grep -qE '^[[:space:]]+retention-days:[[:space:]]*20[[:space:]]*$' <<<"$AGENT_JOB"; then
  pass "the agent artifact is kept for 20 days"
else
  fail "the agent artifact does not carry the 20-day retention the SBOM and the agent release use"
fi

if grep -qF 'actions/upload-artifact@' <<<"$AGENT_JOB"; then
  pass "build-agent uploads the binary as an artifact"
else
  fail "build-agent uploads the binary as an artifact"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
