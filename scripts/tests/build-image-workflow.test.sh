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

# --- the agent the staging deploy runs its machines on -----------------------
#
# The deploy's cache token carries read scope only, so a toolchain cache there
# is never warm and every save is refused while the step reports success. This
# workflow checks out the same commit the deploy rolls out and its token does
# write, so the binary is built here and the deploy downloads it.

# The body of a top-level job, from its key to the next job key.
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

# The deploy needs the binary on every run, including the ~80% that change no
# image input and take the tag-forward path. Gating this on image_changed would
# leave those runs with nothing to download.
if grep -qF 'image_changed' <<<"$AGENT_JOB"; then
  fail "build-agent is gated on image_changed, so the tag-forward path leaves the deploy with no binary"
else
  pass "build-agent is not gated on image_changed"
fi

# A deploy reaching back into this run has to find the artifact still there.
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
