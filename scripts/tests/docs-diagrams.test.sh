#!/usr/bin/env bash
# Tests for the docs-as-code diagram policy.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

assert_contains() {
  local name="$1"
  local file="$2"
  local needle="$3"

  if grep -qF "$needle" "$file"; then
    pass "$name"
  else
    fail "$name"
  fi
}

assert_mermaid_count_at_least() {
  local file="$1"
  local minimum="$2"
  local count

  count="$(grep -c '^```mermaid$' "$file" || true)"
  if [ "$count" -ge "$minimum" ]; then
    pass "${file#"$REPO_ROOT/"} has at least $minimum Mermaid blocks"
  else
    fail "${file#"$REPO_ROOT/"} has at least $minimum Mermaid blocks (got $count)"
  fi
}

echo "docs-diagrams:"

assert_mermaid_count_at_least "$REPO_ROOT/docs/Architecture.md" 3

if grep -q '^```mermaid$' "$REPO_ROOT/docs/Multiscale-Readiness.md"; then
  pass "Multiscale readiness keeps the house Mermaid style"
else
  fail "Multiscale readiness keeps the house Mermaid style"
fi

if diagram_blobs="$(git -C "$REPO_ROOT" ls-files ':(glob)docs/**/*.svg' ':(glob)docs/**/*.d2')" \
  && [ -z "$diagram_blobs" ]; then
  pass "docs do not commit rendered SVG or D2 diagram sources"
else
  fail "docs do not commit rendered SVG or D2 diagram sources"
fi

if rendered_diagrams="$(grep -RInE '^```(d2|dot|graphviz)$|<svg|!\[[^]]*\]\([^)]*\.svg' "$REPO_ROOT/docs" || true)" \
  && [ -z "$rendered_diagrams" ]; then
  pass "docs use Mermaid fences instead of rendered diagram blobs"
else
  fail "docs use Mermaid fences instead of rendered diagram blobs"
fi

assert_contains \
  "docs README records Mermaid-only diagram convention" \
  "$REPO_ROOT/docs/README.md" \
  "Use Mermaid fenced blocks for diagrams"

assert_contains \
  "precommit runs go-arch-lint" \
  "$REPO_ROOT/scripts/precommit-gauntlet.sh" \
  'run_check "go-arch-lint"'
assert_contains \
  "precommit runs cargo modules snapshot" \
  "$REPO_ROOT/scripts/precommit-gauntlet.sh" \
  'run_check "cargo modules"'
assert_contains \
  "precommit runs dependency-cruiser" \
  "$REPO_ROOT/scripts/precommit-gauntlet.sh" \
  'run_check "depcruise"'

assert_contains \
  "CI runs go-arch-lint" \
  "$REPO_ROOT/.github/workflows/ci.yml" \
  "go-arch-lint check"
assert_contains \
  "CI runs cargo modules snapshot" \
  "$REPO_ROOT/.github/workflows/ci.yml" \
  "cargo modules snapshot diff"
assert_contains \
  "CI runs dependency-cruiser snapshot" \
  "$REPO_ROOT/.github/workflows/ci.yml" \
  "depcruise snapshot check"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
