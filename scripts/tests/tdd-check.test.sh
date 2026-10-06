#!/usr/bin/env bash
# Tests for scripts/tdd-check.sh.
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

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

assert_ok() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "$name"; else fail "$name (expected exit 0, got $?)"; fi
}
assert_fail() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then fail "$name (expected non-zero exit, got 0)"; else pass "$name"; fi
}

# Creates a temp git repo with one commit on dev, sets REPO to it and enters it.
make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO"
  git init --quiet --initial-branch=dev
  git config user.email "test@example.com"
  git config user.name "Test"
  echo "base" >base.txt
  git add base.txt
  git commit --quiet -m "init"
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
assert_ok "*.go is source" "$TDD_CHECK" is-source server/internal/api/handlers.go
assert_fail "*_test.go is not source" "$TDD_CHECK" is-source server/internal/api/handlers_test.go
assert_ok "*.rs is source" "$TDD_CHECK" is-source agent/src/main.rs
assert_fail "*_test.rs is not source" "$TDD_CHECK" is-source agent/src/codec_test.rs
assert_ok "*.tsx is source" "$TDD_CHECK" is-source web/src/App.tsx
assert_fail "*.test.tsx is not source" "$TDD_CHECK" is-source web/src/Foo.test.tsx
assert_fail "*.test.ts is not source" "$TDD_CHECK" is-source web/src/foo.test.ts
assert_fail "*_spec.ts is not source" "$TDD_CHECK" is-source web/src/foo_spec.ts
assert_fail "openapi_gen.go is not source" "$TDD_CHECK" is-source server/internal/api/openapi_gen.go
assert_fail "*_gen.go is not source" "$TDD_CHECK" is-source server/internal/foo_gen.go
assert_fail "*.pb.go is not source" "$TDD_CHECK" is-source server/internal/foo.pb.go
assert_fail "*.md is not source" "$TDD_CHECK" is-source docs/Home.md
assert_fail "*.json is not source" "$TDD_CHECK" is-source package.json
assert_fail "files under tests/ not source" "$TDD_CHECK" is-source server/tests/integration/foo.go
assert_fail "files under /test/ not source" "$TDD_CHECK" is-source server/test/foo.go
assert_fail "files under __tests__/ not source" "$TDD_CHECK" is-source web/src/__tests__/foo.ts

echo
echo "is-code classifier (tests INCLUDED, generated EXCLUDED):"
assert_ok "*.go is code" "$TDD_CHECK" is-code server/internal/api/handlers.go
assert_ok "*_test.go IS code" "$TDD_CHECK" is-code server/internal/api/handlers_test.go
assert_ok "*.rs is code" "$TDD_CHECK" is-code agent/src/main.rs
assert_ok "build.rs is code" "$TDD_CHECK" is-code agent/crates/mesh-agent/build.rs
assert_ok "*.test.tsx IS code" "$TDD_CHECK" is-code web/src/Foo.test.tsx
assert_ok "*_spec.ts IS code" "$TDD_CHECK" is-code web/src/foo_spec.ts
assert_ok "files under tests/ ARE code" "$TDD_CHECK" is-code server/tests/integration/foo.go
assert_ok "files under e2e/ ARE code" "$TDD_CHECK" is-code web/e2e/login.spec.ts
assert_fail "openapi_gen.go is not code" "$TDD_CHECK" is-code server/internal/api/openapi_gen.go
assert_fail "*_gen.go is not code" "$TDD_CHECK" is-code server/internal/foo_gen.go
assert_fail "*.pb.go is not code" "$TDD_CHECK" is-code server/internal/foo.pb.go
assert_fail "*.md is not code" "$TDD_CHECK" is-code docs/Home.md
assert_fail "*.json is not code" "$TDD_CHECK" is-code package.json
assert_fail "*.sh is not code" "$TDD_CHECK" is-code scripts/foo.sh
assert_fail "*.yml is not code" "$TDD_CHECK" is-code .github/workflows/ci.yml

echo
echo "has-test-change (branch state):"

make_repo
assert_fail "fresh branch with no changes has no test change" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/foo_test.go
assert_ok "untracked _test.go counts as test change" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/bar_test.go
git add server/internal/api/bar_test.go
assert_ok "staged _test.go counts as test change" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/baz_test.go
git add server/internal/api/baz_test.go
git commit --quiet -m "add test"
assert_ok "committed _test.go on branch counts" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/qux_test.go
git add server/internal/api/qux_test.go
git commit --quiet -m "seed"
git checkout --quiet dev
git merge --quiet --no-ff feat/test -m "merge"
git checkout --quiet -b feat/test2
echo "// edit" >>server/internal/api/qux_test.go
assert_ok "unstaged change to tracked _test.go counts" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/source.go
git add server/internal/api/source.go
git commit --quiet -m "src only"
assert_fail "source-only branch has no test change" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p web/e2e
echo "test" >web/e2e/login.spec.ts
assert_ok "web/e2e/ path counts as test" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
mkdir -p server/tests/integration
echo "package x" >server/tests/integration/x.go
assert_ok "tests/ directory file counts" "$TDD_CHECK" has-test-change
cleanup_repo

# Commits a file on dev and branches off, so the branch diff holds only the later change.
seed_on_base() {
  git add "$1"
  git commit --quiet -m "seed $1"
  git checkout --quiet -b feat/inline
}

make_repo
git checkout --quiet dev
mkdir -p agent/src
cat >agent/src/thing.rs <<'RS'
pub fn double(n: u32) -> u32 {
    n * 2
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn doubles() {
        assert_eq!(double(2), 4);
    }
}
RS
seed_on_base agent/src/thing.rs
python3 - <<'PYEOF'
import pathlib

p = pathlib.Path("agent/src/thing.rs")
p.write_text(
    p.read_text().replace(
        "    #[test]\n    fn doubles() {",
        "    #[test]\n    fn doubles_zero() {\n        assert_eq!(double(0), 0);\n    }\n\n"
        "    #[test]\n    fn doubles() {",
    )
)
PYEOF
assert_ok "a branch change inside a Rust inline test module counts" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
git checkout --quiet dev
mkdir -p agent/src
cat >agent/src/thing.rs <<'RS'
pub fn double(n: u32) -> u32 {
    n * 2
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn doubles() {
        assert_eq!(double(2), 4);
    }
}
RS
seed_on_base agent/src/thing.rs
python3 - <<'PYEOF'
import pathlib

p = pathlib.Path("agent/src/thing.rs")
s = p.read_text().replace("    n * 2", "    n.wrapping_mul(2)")
s = s.replace(
    "        assert_eq!(double(2), 4);",
    "        assert_eq!(double(2), 4);\n        assert_eq!(double(3), 6);",
)
p.write_text(s)
PYEOF
assert_fail "a Rust file changed above its test module does not count" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
git checkout --quiet dev
mkdir -p agent/src
printf 'pub fn one() -> u32 {\n    1\n}\n' >agent/src/plain.rs
seed_on_base agent/src/plain.rs
printf '\npub fn two() -> u32 {\n    2\n}\n' >>agent/src/plain.rs
assert_fail "a Rust file with no inline test module does not count" "$TDD_CHECK" has-test-change
cleanup_repo

make_repo
git checkout --quiet dev
mkdir -p server/internal/api
printf 'package api\n\nfunc One() int { return 1 }\n' >server/internal/api/one.go
seed_on_base server/internal/api/one.go
printf '\nfunc Two() int { return 2 }\n' >>server/internal/api/one.go
assert_fail "a Go source change is not excused by any inline block" "$TDD_CHECK" has-test-change
cleanup_repo

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
