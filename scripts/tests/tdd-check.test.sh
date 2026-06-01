#!/usr/bin/env bash
# Tests for scripts/tdd-check.sh. Plain bash; no bats dependency.
# Run: ./scripts/tests/tdd-check.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TDD_CHECK="$SCRIPT_DIR/../tdd-check.sh"

if [ ! -x "$TDD_CHECK" ]; then
  echo "FAIL: $TDD_CHECK not found or not executable" >&2
  exit 1
fi

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

# Run "$@" and assert exit 0.
assert_ok() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then pass "$name"; else fail "$name (expected exit 0, got $?)"; fi
}
# Run "$@" and assert non-zero exit.
assert_fail() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then fail "$name (expected non-zero exit, got 0)"; else pass "$name"; fi
}

# Build a temp git repo seeded with one initial commit on "dev".
# Sets REPO to the path; cd's into it. Caller must trap-cleanup REPO.
make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO"
  git init --quiet --initial-branch=dev
  git config user.email "test@example.com"
  git config user.name "Test"
  echo "base" > base.txt
  git add base.txt
  git commit --quiet -m "init"
  # Create a feature branch (so HEAD..origin/dev would be empty by merge-base fallback).
  git checkout --quiet -b feat/test
}

cleanup_repo() {
  if [ -n "${REPO:-}" ]; then
    rm -rf "$REPO"
    REPO=""
  fi
  return 0
}
trap 'cleanup_repo' EXIT

echo "is-source classifier:"
assert_ok   "*.go is source"          "$TDD_CHECK" is-source server/internal/api/handlers.go
assert_fail "*_test.go is not source" "$TDD_CHECK" is-source server/internal/api/handlers_test.go
assert_ok   "*.rs is source"          "$TDD_CHECK" is-source agent/src/main.rs
assert_fail "*_test.rs is not source" "$TDD_CHECK" is-source agent/src/codec_test.rs
assert_ok   "*.tsx is source"         "$TDD_CHECK" is-source web/src/App.tsx
assert_fail "*.test.tsx is not source" "$TDD_CHECK" is-source web/src/Foo.test.tsx
assert_fail "*.test.ts is not source"  "$TDD_CHECK" is-source web/src/foo.test.ts
assert_fail "*_spec.ts is not source"  "$TDD_CHECK" is-source web/src/foo_spec.ts
assert_fail "openapi_gen.go is not source" "$TDD_CHECK" is-source server/internal/api/openapi_gen.go
assert_fail "*_gen.go is not source"   "$TDD_CHECK" is-source server/internal/foo_gen.go
assert_fail "*.pb.go is not source"    "$TDD_CHECK" is-source server/internal/foo.pb.go
assert_fail "*.md is not source"       "$TDD_CHECK" is-source docs/Home.md
assert_fail "*.json is not source"     "$TDD_CHECK" is-source package.json
assert_fail "files under tests/ not source" "$TDD_CHECK" is-source server/tests/integration/foo.go
assert_fail "files under /test/ not source" "$TDD_CHECK" is-source server/test/foo.go
assert_fail "files under __tests__/ not source" "$TDD_CHECK" is-source web/src/__tests__/foo.ts

echo
echo "is-code classifier (tests INCLUDED, generated EXCLUDED):"
assert_ok   "*.go is code"             "$TDD_CHECK" is-code server/internal/api/handlers.go
assert_ok   "*_test.go IS code"        "$TDD_CHECK" is-code server/internal/api/handlers_test.go
assert_ok   "*.rs is code"             "$TDD_CHECK" is-code agent/src/main.rs
assert_ok   "build.rs is code"         "$TDD_CHECK" is-code agent/crates/mesh-agent/build.rs
assert_ok   "*.test.tsx IS code"       "$TDD_CHECK" is-code web/src/Foo.test.tsx
assert_ok   "*_spec.ts IS code"        "$TDD_CHECK" is-code web/src/foo_spec.ts
assert_ok   "files under tests/ ARE code" "$TDD_CHECK" is-code server/tests/integration/foo.go
assert_ok   "files under e2e/ ARE code"   "$TDD_CHECK" is-code web/e2e/login.spec.ts
assert_fail "openapi_gen.go is not code"  "$TDD_CHECK" is-code server/internal/api/openapi_gen.go
assert_fail "*_gen.go is not code"        "$TDD_CHECK" is-code server/internal/foo_gen.go
assert_fail "*.pb.go is not code"         "$TDD_CHECK" is-code server/internal/foo.pb.go
assert_fail "*.md is not code"            "$TDD_CHECK" is-code docs/Home.md
assert_fail "*.json is not code"          "$TDD_CHECK" is-code package.json
assert_fail "*.sh is not code"            "$TDD_CHECK" is-code scripts/foo.sh
assert_fail "*.yml is not code"           "$TDD_CHECK" is-code .github/workflows/ci.yml

echo
echo "has-test-change (branch state):"

# 1. Clean fresh branch off dev — no test change.
make_repo
assert_fail "fresh branch with no changes has no test change" "$TDD_CHECK" has-test-change
cleanup_repo

# 2. Untracked test file → has test change.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
assert_ok "untracked _test.go counts as test change" "$TDD_CHECK" has-test-change
cleanup_repo

# 3. Staged test file → has test change.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/bar_test.go
git add server/internal/api/bar_test.go
assert_ok "staged _test.go counts as test change" "$TDD_CHECK" has-test-change
cleanup_repo

# 4. Committed test file on branch → has test change.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/baz_test.go
git add server/internal/api/baz_test.go
git commit --quiet -m "add test"
assert_ok "committed _test.go on branch counts" "$TDD_CHECK" has-test-change
cleanup_repo

# 5. Unstaged change to an existing tracked test file → has test change.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/qux_test.go
git add server/internal/api/qux_test.go
git commit --quiet -m "seed"
git checkout --quiet dev
git merge --quiet --no-ff feat/test -m "merge"
git checkout --quiet -b feat/test2
echo "// edit" >> server/internal/api/qux_test.go
assert_ok "unstaged change to tracked _test.go counts" "$TDD_CHECK" has-test-change
cleanup_repo

# 6. Only source committed on branch — no test change.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/source.go
git add server/internal/api/source.go
git commit --quiet -m "src only"
assert_fail "source-only branch has no test change" "$TDD_CHECK" has-test-change
cleanup_repo

# 7. web/e2e/ path counts as test.
make_repo
mkdir -p web/e2e
echo "test" > web/e2e/login.spec.ts
assert_ok "web/e2e/ path counts as test" "$TDD_CHECK" has-test-change
cleanup_repo

# 8. tests/ directory counts.
make_repo
mkdir -p server/tests/integration
echo "package x" > server/tests/integration/x.go
assert_ok "tests/ directory file counts" "$TDD_CHECK" has-test-change
cleanup_repo

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
