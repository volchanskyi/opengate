#!/usr/bin/env bash
# Holds the workflows to .claude/rules/ci-cd-determinism.md: a CI/CD step whose
# work was refused must not report success.
#
# Bug history: the deploy's cache token carries read scope only. Two saves were
# refused on every run for two months — the deploy-state entry the pre-flight
# skip stood on, and a toolchain cache added later that never once wrote — and
# both steps were green. A 45.8% skip rate went to zero and nothing said so.
#
# Run: ./scripts/tests/ci-cd-determinism.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOWS="$REPO_ROOT/.github/workflows"
RULE="$REPO_ROOT/.claude/rules/ci-cd-determinism.md"
GUARD="scripts/assert-cache-written.sh"

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

echo "ci-cd-determinism:"

# --- the rule is written down and indexed -----------------------------------
if [ -f "$RULE" ]; then
  pass "the rule exists"
else
  fail "the rule exists"
fi
if grep -qF 'rules/ci-cd-determinism.md' "$REPO_ROOT/CLAUDE.md"; then
  pass "the rule is indexed in CLAUDE.md"
else
  fail "the rule is not indexed in CLAUDE.md, so nothing points a reader at it"
fi
if [ -x "$REPO_ROOT/$GUARD" ]; then
  pass "the read-back guard is executable"
else
  fail "the read-back guard is executable"
fi

# --- the deploy asks for no cache at all ------------------------------------
#
# Its token cannot write. Every cache it declares is a save that will be refused
# and reported as a success, so it declares none — not an explicit cache action,
# not a toolchain cache, and not the cache half of a setup action.
CD="$WORKFLOWS/cd.yml"
for shape in 'actions/cache' 'Swatinem/rust-cache' 'cache-to:' '^[[:space:]]*cache:[[:space:]]'; do
  if grep -qE -- "$shape" "$CD"; then
    fail "cd.yml declares a cache ($shape); its token cannot write, so that save is refused and reported as a success"
  else
    pass "cd.yml declares no cache ($shape)"
  fi
done

# --- every cache write we name is read back ---------------------------------
#
# An inline save names its own key, so nothing stops the same key being asserted
# in the step after it. A save with no read-back is the defect this rule is about.
SAVE_HITS=0
for wf in "$WORKFLOWS"/*.yml; do
  grep -qF 'actions/cache/save@' "$wf" || continue
  SAVE_HITS=$((SAVE_HITS + 1))
  name="$(basename "$wf")"
  if grep -qF "$GUARD" "$wf"; then
    pass "$name reads back the cache entry it writes"
  else
    fail "$name writes a cache entry and never reads it back"
  fi
done
if [ "$SAVE_HITS" -eq 0 ]; then
  pass "no workflow writes a cache entry inline"
fi

# --- the agent build's cache is read back -----------------------------------
#
# The deploy no longer builds the agent; the image workflow does, and it is that
# workflow's cache keeping the cross-build off a cold start. A refusal there
# would cost three minutes on every run with nothing saying why.
BUILD_IMAGE="$WORKFLOWS/build-image.yml"
if grep -qF "$GUARD" "$BUILD_IMAGE"; then
  pass "build-image reads back the agent build's cache"
else
  fail "build-image never reads back the agent build's cache, so a refusal there is silent"
fi

for target in x86_64-unknown-linux-musl aarch64-unknown-linux-musl; do
  if grep -qF "$target" "$BUILD_IMAGE"; then
    pass "the read-back covers $target"
  else
    fail "the read-back covers $target"
  fi
done

# --- an artifact nobody wrote is not an artifact ------------------------------
#
# Every artifact mutation.yml uploads is an input to the aggregation that scores
# the night. A shard whose tool wrote no report uploads nothing, and the upload
# action's default is to say so in a warning and exit zero — so the shard job is
# green, and the only symptom is the publish job reporting an incomplete set
# without naming what went missing or why. The shard is where the fact is known,
# so the shard is where it has to fail.
MUTATION="$WORKFLOWS/mutation.yml"
uploads="$(grep -c 'uses: actions/upload-artifact' "$MUTATION" || true)"
errors="$(grep -c 'if-no-files-found: error' "$MUTATION" || true)"
if [ "$uploads" -gt 0 ] && [ "$uploads" -eq "$errors" ]; then
  pass "all $uploads mutation.yml artifact uploads fail on an empty file set"
else
  fail "mutation.yml has $uploads artifact upload(s) but only $errors fail on an empty set — a shard that produced no report would report success"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
